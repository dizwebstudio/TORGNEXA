package fx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

var (
	ErrRateStale          = errors.New("fx rate stale")
	ErrConversionOverflow = errors.New("fx conversion overflow")
	ErrSnapshotArithmetic = errors.New("fx conversion snapshot arithmetic mismatch")
	ErrStoreConflict      = errors.New("fx immutable store conflict")
)

// HistoricalStore is global reference-data storage. FX facts contain no tenant data.
type HistoricalStore interface {
	AppendFact(context.Context, RateFact) error
	Candidates(context.Context, LookupRequest, []SourceID) ([]RateFact, error)
	FactByID(context.Context, string) (RateFact, error)
	AppendResolution(context.Context, ResolutionEvidence) error
	AppendConversion(context.Context, ConversionRecord) error
}

type ResolutionEvidence struct {
	ID               string            `json:"id"`
	Pair             Pair              `json:"pair"`
	RateType         RateType          `json:"rate_type"`
	AsOf             domain.UTCInstant `json:"as_of"`
	Precedence       []SourceID        `json:"precedence"`
	CandidateFactIDs []string          `json:"candidate_fact_ids"`
	SelectedFactID   string            `json:"selected_fact_id"`
	ResolvedAt       domain.UTCInstant `json:"resolved_at"`
}

func (e ResolutionEvidence) Validate() error {
	if !factIDPattern.MatchString(e.ID) || e.Pair.Validate() != nil || e.RateType.Validate() != nil || e.AsOf.Validate() != nil || e.ResolvedAt.Validate() != nil || len(e.Precedence) == 0 || !factIDPattern.MatchString(e.SelectedFactID) || len(e.CandidateFactIDs) == 0 {
		return errors.New("invalid FX resolution evidence")
	}
	seen := map[string]bool{}
	for _, s := range e.Precedence {
		if s.Validate() != nil {
			return errors.New("invalid FX resolution evidence")
		}
	}
	for _, id := range e.CandidateFactIDs {
		if !factIDPattern.MatchString(id) || seen[id] {
			return errors.New("invalid FX resolution evidence")
		}
		seen[id] = true
	}
	if !seen[e.SelectedFactID] {
		return errors.New("selected FX fact is not in candidate evidence")
	}
	return nil
}

type FreshnessPolicy struct{ MaxEffectiveAge time.Duration }

func (p FreshnessPolicy) Validate() error {
	if p.MaxEffectiveAge <= 0 || p.MaxEffectiveAge > 31*24*time.Hour {
		return errors.New("invalid FX freshness policy")
	}
	return nil
}
func (p FreshnessPolicy) Check(req LookupRequest, f RateFact) error {
	if p.Validate() != nil || ValidateLookupResult(f.Source(), req, f) != nil {
		return ErrSourceResultMismatch
	}
	age := req.AsOf.Time().Sub(f.EffectiveAt().Time())
	if age < 0 {
		return ErrSourceResultMismatch
	}
	if age > p.MaxEffectiveAge {
		return ErrRateStale
	}
	return nil
}

type cacheEntry struct {
	FactID    string
	ExpiresAt time.Time
}
type MemoryCache struct {
	mu     sync.Mutex
	ttl    time.Duration
	max    int
	values map[string]cacheEntry
	order  []string
}

func NewMemoryCache(ttl time.Duration, max int) *MemoryCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if max < 1 {
		max = 1024
	}
	return &MemoryCache{ttl: ttl, max: max, values: map[string]cacheEntry{}}
}
func (c *MemoryCache) get(k string, now time.Time) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.values[k]
	if !ok || !now.Before(v.ExpiresAt) {
		if ok {
			delete(c.values, k)
		}
		return "", false
	}
	return v.FactID, true
}
func (c *MemoryCache) put(k, id string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.values[k]; !ok {
		c.order = append(c.order, k)
	}
	c.values[k] = cacheEntry{id, now.Add(c.ttl)}
	for len(c.values) > c.max && len(c.order) > 0 {
		old := c.order[0]
		c.order = c.order[1:]
		delete(c.values, old)
	}
}

// Resolver persists provider observations before selecting them. Cache entries contain only persisted fact IDs.
type Resolver struct {
	store      HistoricalStore
	providers  map[SourceID]Provider
	precedence SourcePrecedence
	freshness  map[SourceID]FreshnessPolicy
	cache      *MemoryCache
	now        func() time.Time
}

func NewResolver(store HistoricalStore, providers []Provider, precedence SourcePrecedence, freshness map[SourceID]FreshnessPolicy, cache *MemoryCache, now func() time.Time) (*Resolver, error) {
	if store == nil || precedence.Validate() != nil {
		return nil, errors.New("invalid FX resolver")
	}
	pm := map[SourceID]Provider{}
	for _, p := range providers {
		if p == nil || p.ID().Validate() != nil {
			return nil, errors.New("invalid FX provider")
		}
		pm[p.ID()] = p
	}
	for _, s := range precedence.Sources() {
		fp, ok := freshness[s]
		if !ok || fp.Validate() != nil {
			return nil, errors.New("missing FX freshness policy")
		}
	}
	if cache == nil {
		cache = NewMemoryCache(5*time.Minute, 2048)
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Resolver{store: store, providers: pm, precedence: precedence, freshness: freshness, cache: cache, now: now}, nil
}
func lookupKey(r LookupRequest, p SourcePrecedence) string {
	ss := []string{r.Pair.String(), string(r.RateType), r.AsOf.Time().Format(time.RFC3339Nano)}
	for _, s := range p.Sources() {
		ss = append(ss, s.String())
	}
	return strings.Join(ss, "|")
}
func (r *Resolver) Resolve(ctx context.Context, req LookupRequest) (RateFact, ResolutionEvidence, error) {
	if ctx == nil || req.Validate() != nil {
		return RateFact{}, ResolutionEvidence{}, ErrRateMissing
	}
	now := r.now().UTC()
	key := lookupKey(req, r.precedence)
	if id, ok := r.cache.get(key, now); ok {
		if fact, loadErr := r.store.FactByID(ctx, id); loadErr == nil && r.freshness[fact.Source()].Check(req, fact) == nil {
			// Cache is only an accelerator. Re-read persisted candidates so a newly
			// arrived higher-precedence fact cannot be hidden by a process-local hit.
			if cachedCandidates, candidatesErr := r.store.Candidates(ctx, req, r.precedence.Sources()); candidatesErr == nil {
				if selected, selectErr := r.precedence.Select(req, cachedCandidates); selectErr == nil && selected.ID() == fact.ID() && r.freshness[selected.Source()].Check(req, selected) == nil {
					ev := r.evidence(req, cachedCandidates, selected, now)
					if appendErr := r.store.AppendResolution(ctx, ev); appendErr == nil {
						return selected, ev, nil
					}
				}
			}
		}
	}
	facts, err := r.store.Candidates(ctx, req, r.precedence.Sources())
	if err != nil {
		return RateFact{}, ResolutionEvidence{}, err
	}
	if selected, e := r.precedence.Select(req, facts); e == nil && r.freshness[selected.Source()].Check(req, selected) == nil {
		ev := r.evidence(req, facts, selected, now)
		if err = r.store.AppendResolution(ctx, ev); err != nil {
			return RateFact{}, ResolutionEvidence{}, err
		}
		r.cache.put(key, selected.ID(), now)
		return selected, ev, nil
	}
	// Refresh every configured source in precedence order; provider errors never permit stale fallback.
	for _, source := range r.precedence.Sources() {
		p := r.providers[source]
		if p == nil {
			continue
		}
		f, e := p.Lookup(ctx, req)
		if e != nil {
			continue
		}
		if ValidateLookupResult(source, req, f) != nil {
			continue
		}
		if e = r.store.AppendFact(ctx, f); e != nil && !errors.Is(e, ErrStoreConflict) {
			return RateFact{}, ResolutionEvidence{}, e
		}
	}
	facts, err = r.store.Candidates(ctx, req, r.precedence.Sources())
	if err != nil {
		return RateFact{}, ResolutionEvidence{}, err
	}
	selected, err := r.precedence.Select(req, facts)
	if err != nil {
		return RateFact{}, ResolutionEvidence{}, err
	}
	if err = r.freshness[selected.Source()].Check(req, selected); err != nil {
		return RateFact{}, ResolutionEvidence{}, err
	}
	ev := r.evidence(req, facts, selected, now)
	if err = r.store.AppendResolution(ctx, ev); err != nil {
		return RateFact{}, ResolutionEvidence{}, err
	}
	r.cache.put(key, selected.ID(), now)
	return selected, ev, nil
}
func (r *Resolver) evidence(req LookupRequest, facts []RateFact, selected RateFact, now time.Time) ResolutionEvidence {
	ids := make([]string, 0, len(facts))
	for _, f := range facts {
		if f.Pair() == req.Pair && f.RateType() == req.RateType && !f.EffectiveAt().Time().After(req.AsOf.Time()) {
			ids = append(ids, f.ID())
		}
	}
	sort.Strings(ids)
	instant, _ := domain.NewUTCInstant(now)
	h := sha256.Sum256([]byte(req.Pair.String() + "|" + req.AsOf.Time().Format(time.RFC3339Nano) + "|" + selected.ID() + "|" + strings.Join(ids, ",")))
	return ResolutionEvidence{ID: "fxres_" + hex.EncodeToString(h[:12]), Pair: req.Pair, RateType: req.RateType, AsOf: req.AsOf, Precedence: r.precedence.Sources(), CandidateFactIDs: ids, SelectedFactID: selected.ID(), ResolvedAt: instant}
}

type ConversionRequest struct {
	ID                   string
	Source               domain.Money
	SourceMinorUnitScale uint8
	TargetCurrency       domain.Currency
	TargetMinorUnitScale uint8
	AsOf                 domain.UTCInstant
	RateType             RateType
	Rounding             RoundingPolicy
	Triangulation        TriangulationPolicy
}

func (r ConversionRequest) Validate() error {
	if !factIDPattern.MatchString(r.ID) || r.Source.Validate() != nil || r.TargetCurrency.Validate() != nil || r.Source.Currency() == r.TargetCurrency || r.SourceMinorUnitScale > domain.MaxDecimalScale || r.TargetMinorUnitScale > domain.MaxDecimalScale || r.AsOf.Validate() != nil || r.RateType.Validate() != nil || r.Rounding.Validate() != nil || r.Triangulation.Validate() != nil {
		return errors.New("invalid FX conversion request")
	}
	return nil
}

type ConversionRecord struct {
	ID                    string             `json:"id"`
	Snapshot              ConversionSnapshot `json:"snapshot"`
	SourceMinorUnitScale  uint8              `json:"source_minor_unit_scale"`
	ResolutionEvidenceIDs []string           `json:"resolution_evidence_ids"`
	Digest                string             `json:"digest"`
}

func (r ConversionRecord) Validate() error {
	if !factIDPattern.MatchString(r.ID) || r.SourceMinorUnitScale > domain.MaxDecimalScale || r.Snapshot.Validate() != nil || len(r.ResolutionEvidenceIDs) != len(r.Snapshot.RateFacts) || len(r.Digest) != 64 {
		return errors.New("invalid FX conversion record")
	}
	for _, id := range r.ResolutionEvidenceIDs {
		if !factIDPattern.MatchString(id) {
			return errors.New("invalid FX conversion record")
		}
	}
	return nil
}
func (r *Resolver) Convert(ctx context.Context, req ConversionRequest) (ConversionRecord, error) {
	if req.Validate() != nil {
		return ConversionRecord{}, errors.New("invalid FX conversion request")
	}
	route, err := req.Triangulation.Route(req.Source.Currency(), req.TargetCurrency)
	if err != nil {
		return ConversionRecord{}, err
	}
	facts := make([]RateFact, 0, len(route))
	evidence := make([]string, 0, len(route))
	for _, pair := range route {
		f, e, er := r.Resolve(ctx, LookupRequest{Pair: pair, AsOf: req.AsOf, RateType: req.RateType})
		if er != nil {
			return ConversionRecord{}, er
		}
		facts = append(facts, f)
		evidence = append(evidence, e.ID)
	}
	minor, err := convertMinor(req.Source.MinorUnits(), req.SourceMinorUnitScale, req.TargetMinorUnitScale, facts, req.Rounding.Mode())
	if err != nil {
		return ConversionRecord{}, err
	}
	target, err := domain.NewMoney(minor, req.TargetCurrency)
	if err != nil {
		return ConversionRecord{}, err
	}
	derived, _ := domain.NewUTCInstant(r.now().UTC())
	snapshot, err := NewConversionSnapshot(req.Source, target, facts, req.Rounding, req.Triangulation, req.TargetMinorUnitScale, derived)
	if err != nil {
		return ConversionRecord{}, err
	}
	if err = VerifySnapshotArithmetic(snapshot, req.SourceMinorUnitScale); err != nil {
		return ConversionRecord{}, err
	}
	digestInput, err := json.Marshal(struct {
		Snapshot             ConversionSnapshot `json:"snapshot"`
		SourceMinorUnitScale uint8              `json:"source_minor_unit_scale"`
		Evidence             []string           `json:"resolution_evidence_ids"`
	}{snapshot, req.SourceMinorUnitScale, evidence})
	if err != nil {
		return ConversionRecord{}, err
	}
	sum := sha256.Sum256(digestInput)
	rec := ConversionRecord{ID: req.ID, Snapshot: snapshot, SourceMinorUnitScale: req.SourceMinorUnitScale, ResolutionEvidenceIDs: evidence, Digest: hex.EncodeToString(sum[:])}
	if err = r.store.AppendConversion(ctx, rec); err != nil {
		return ConversionRecord{}, err
	}
	return rec, nil
}
func VerifySnapshotArithmetic(s ConversionSnapshot, sourceScale uint8) error {
	if s.Validate() != nil || sourceScale > domain.MaxDecimalScale {
		return ErrSnapshotArithmetic
	}
	got, err := convertMinor(s.SourceAmount.MinorUnits(), sourceScale, s.TargetMinorUnitScale, s.RateFacts, s.RoundingMode)
	if err != nil {
		return err
	}
	if got != s.TargetAmount.MinorUnits() {
		return ErrSnapshotArithmetic
	}
	return nil
}
func convertMinor(source int64, sourceScale, targetScale uint8, facts []RateFact, mode RoundingMode) (int64, error) {
	n := big.NewInt(source)
	d := big.NewInt(1)
	for _, f := range facts {
		if f.Validate() != nil {
			return 0, ErrSnapshotArithmetic
		}
		n.Mul(n, big.NewInt(f.Rate().Coefficient()))
		d.Mul(d, pow10(int(f.Rate().Scale())))
	}
	n.Mul(n, pow10(int(targetScale)))
	d.Mul(d, pow10(int(sourceScale)))
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(n, d, rem)
	if mode != RoundTowardZero && rem.Sign() != 0 {
		absRem := new(big.Int).Abs(rem)
		twice := new(big.Int).Lsh(absRem, 1)
		cmp := twice.Cmp(d)
		bump := cmp > 0 || (cmp == 0 && (mode == RoundHalfUp || (mode == RoundHalfEven && new(big.Int).Abs(q).Bit(0) == 1)))
		if bump {
			if n.Sign() < 0 {
				q.Sub(q, big.NewInt(1))
			} else {
				q.Add(q, big.NewInt(1))
			}
		}
	}
	if !q.IsInt64() {
		return 0, ErrConversionOverflow
	}
	return q.Int64(), nil
}
func pow10(n int) *big.Int      { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }
func HasConversionEngine() bool { return true }

// MemoryStore is a deterministic reference store used by tests and local qualification.
type MemoryStore struct {
	mu          sync.Mutex
	facts       map[string]RateFact
	resolutions map[string]ResolutionEvidence
	conversions map[string]ConversionRecord
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{facts: map[string]RateFact{}, resolutions: map[string]ResolutionEvidence{}, conversions: map[string]ConversionRecord{}}
}
func (s *MemoryStore) AppendFact(_ context.Context, f RateFact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f.Validate() != nil {
		return ErrStoreConflict
	}
	if old, ok := s.facts[f.ID()]; ok {
		a, _ := old.MarshalJSON()
		b, _ := f.MarshalJSON()
		if string(a) != string(b) {
			return ErrStoreConflict
		}
		return nil
	}
	s.facts[f.ID()] = f
	return nil
}
func (s *MemoryStore) Candidates(_ context.Context, r LookupRequest, sources []SourceID) ([]RateFact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	allowed := map[SourceID]bool{}
	for _, x := range sources {
		allowed[x] = true
	}
	out := []RateFact{}
	for _, f := range s.facts {
		if f.Pair() == r.Pair && f.RateType() == r.RateType && !f.EffectiveAt().Time().After(r.AsOf.Time()) && allowed[f.Source()] {
			out = append(out, f)
		}
	}
	return out, nil
}
func (s *MemoryStore) FactByID(_ context.Context, id string) (RateFact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.facts[id]
	if !ok {
		return RateFact{}, ErrRateMissing
	}
	return f, nil
}
func (s *MemoryStore) AppendResolution(_ context.Context, e ResolutionEvidence) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Validate() != nil {
		return ErrStoreConflict
	}
	if old, ok := s.resolutions[e.ID]; ok {
		a, _ := json.Marshal(old)
		b, _ := json.Marshal(e)
		if string(a) != string(b) {
			return ErrStoreConflict
		}
		return nil
	}
	s.resolutions[e.ID] = e
	return nil
}
func (s *MemoryStore) AppendConversion(_ context.Context, r ConversionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Validate() != nil {
		return ErrStoreConflict
	}
	if old, ok := s.conversions[r.ID]; ok && old.Digest != r.Digest {
		return ErrStoreConflict
	}
	s.conversions[r.ID] = r
	return nil
}
