package fx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

const (
	SchemaVersion           uint16 = 1
	MaxSourceReferenceBytes        = 256
	MaxSnapshotRateFacts           = 2
)

var (
	ErrRateMissing                     = errors.New("fx rate missing")
	ErrSourceResultMismatch            = errors.New("fx provider result mismatch")
	ErrCrossCurrencyConversionDisabled = errors.New("cross-currency conversion is disabled until Task 089b")

	factIDPattern          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	sourceIDPattern        = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	sourceReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)
)

// Pair is an ordered currency pair. No implicit inversion is ever performed.
type Pair struct {
	Base  domain.Currency `json:"base_currency"`
	Quote domain.Currency `json:"quote_currency"`
}

func NewPair(base, quote domain.Currency) (Pair, error) {
	if err := base.Validate(); err != nil {
		return Pair{}, fmt.Errorf("base currency: %w", err)
	}
	if err := quote.Validate(); err != nil {
		return Pair{}, fmt.Errorf("quote currency: %w", err)
	}
	if base == quote {
		return Pair{}, errors.New("fx pair currencies must differ")
	}
	return Pair{Base: base, Quote: quote}, nil
}

func (p Pair) Validate() error {
	_, err := NewPair(p.Base, p.Quote)
	return err
}

func (p Pair) String() string { return p.Base.String() + "/" + p.Quote.String() }

// SourceID identifies a provider-neutral source in deterministic precedence policy.
type SourceID string

func NewSourceID(value string) (SourceID, error) {
	if !sourceIDPattern.MatchString(value) {
		return "", errors.New("FX source id must be a canonical lowercase identifier")
	}
	return SourceID(value), nil
}

func (s SourceID) String() string { return string(s) }
func (s SourceID) Validate() error {
	_, err := NewSourceID(string(s))
	return err
}

// RateType describes the semantics of a source quote rather than a provider name.
type RateType string

const (
	RateOfficial   RateType = "official"
	RateMid        RateType = "mid"
	RateBid        RateType = "bid"
	RateAsk        RateType = "ask"
	RateClosing    RateType = "closing"
	RateIndicative RateType = "indicative"
)

func (r RateType) Validate() error {
	switch r {
	case RateOfficial, RateMid, RateBid, RateAsk, RateClosing, RateIndicative:
		return nil
	default:
		return errors.New("unsupported FX rate type")
	}
}

// RateFactInput is accepted only by NewRateFact; RateFact itself exposes no setters.
type RateFactInput struct {
	ID              string
	Pair            Pair
	Rate            domain.Decimal
	Source          SourceID
	SourceReference string
	RateType        RateType
	ObservedAt      domain.UTCInstant
	EffectiveAt     domain.UTCInstant
}

// RateFact is an immutable sourced FX observation. A new market observation is
// represented by a new fact ID rather than by mutating an existing fact.
type RateFact struct {
	id              string
	pair            Pair
	rate            domain.Decimal
	source          SourceID
	sourceReference string
	rateType        RateType
	observedAt      domain.UTCInstant
	effectiveAt     domain.UTCInstant
}

func NewRateFact(input RateFactInput) (RateFact, error) {
	if !factIDPattern.MatchString(input.ID) {
		return RateFact{}, errors.New("FX rate fact id is not canonical")
	}
	if err := input.Pair.Validate(); err != nil {
		return RateFact{}, err
	}
	if err := input.Rate.Validate(); err != nil || input.Rate.Coefficient() <= 0 {
		return RateFact{}, errors.New("FX rate must be a positive canonical decimal")
	}
	if err := input.Source.Validate(); err != nil {
		return RateFact{}, err
	}
	if input.SourceReference != "" {
		if len(input.SourceReference) > MaxSourceReferenceBytes || !sourceReferencePattern.MatchString(input.SourceReference) || containsCredentialLikeText(input.SourceReference) {
			return RateFact{}, errors.New("FX source reference must be a bounded opaque non-secret identifier")
		}
	}
	if err := input.RateType.Validate(); err != nil {
		return RateFact{}, err
	}
	if err := input.ObservedAt.Validate(); err != nil {
		return RateFact{}, fmt.Errorf("observed_at: %w", err)
	}
	if err := input.EffectiveAt.Validate(); err != nil {
		return RateFact{}, fmt.Errorf("effective_at: %w", err)
	}
	return RateFact{
		id:              input.ID,
		pair:            input.Pair,
		rate:            input.Rate,
		source:          input.Source,
		sourceReference: input.SourceReference,
		rateType:        input.RateType,
		observedAt:      input.ObservedAt,
		effectiveAt:     input.EffectiveAt,
	}, nil
}

func (f RateFact) ID() string                     { return f.id }
func (f RateFact) Pair() Pair                     { return f.pair }
func (f RateFact) Rate() domain.Decimal           { return f.rate }
func (f RateFact) Source() SourceID               { return f.source }
func (f RateFact) SourceReference() string        { return f.sourceReference }
func (f RateFact) RateType() RateType             { return f.rateType }
func (f RateFact) ObservedAt() domain.UTCInstant  { return f.observedAt }
func (f RateFact) EffectiveAt() domain.UTCInstant { return f.effectiveAt }
func (f RateFact) Validate() error {
	_, err := NewRateFact(RateFactInput{
		ID: f.id, Pair: f.pair, Rate: f.rate, Source: f.source,
		SourceReference: f.sourceReference, RateType: f.rateType,
		ObservedAt: f.observedAt, EffectiveAt: f.effectiveAt,
	})
	return err
}

func (f RateFact) MarshalJSON() ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(rateFactWire{
		SchemaVersion:   SchemaVersion,
		ID:              f.id,
		BaseCurrency:    f.pair.Base,
		QuoteCurrency:   f.pair.Quote,
		Rate:            f.rate,
		Source:          f.source,
		SourceReference: f.sourceReference,
		RateType:        f.rateType,
		ObservedAt:      f.observedAt,
		EffectiveAt:     f.effectiveAt,
	})
}

func (f *RateFact) UnmarshalJSON(data []byte) error {
	var wire rateFactWire
	if err := decodeStrictJSON(data, &wire); err != nil {
		return err
	}
	if wire.SchemaVersion != SchemaVersion {
		return errors.New("unsupported FX rate fact schema version")
	}
	pair, err := NewPair(wire.BaseCurrency, wire.QuoteCurrency)
	if err != nil {
		return err
	}
	value, err := NewRateFact(RateFactInput{
		ID: wire.ID, Pair: pair, Rate: wire.Rate, Source: wire.Source,
		SourceReference: wire.SourceReference, RateType: wire.RateType,
		ObservedAt: wire.ObservedAt, EffectiveAt: wire.EffectiveAt,
	})
	if err != nil {
		return err
	}
	*f = value
	return nil
}

type rateFactWire struct {
	SchemaVersion   uint16            `json:"schema_version"`
	ID              string            `json:"id"`
	BaseCurrency    domain.Currency   `json:"base_currency"`
	QuoteCurrency   domain.Currency   `json:"quote_currency"`
	Rate            domain.Decimal    `json:"rate"`
	Source          SourceID          `json:"source"`
	SourceReference string            `json:"source_reference,omitempty"`
	RateType        RateType          `json:"rate_type"`
	ObservedAt      domain.UTCInstant `json:"observed_at"`
	EffectiveAt     domain.UTCInstant `json:"effective_at"`
}

func decodeStrictJSON(data []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("JSON contains trailing value")
		}
		return err
	}
	return nil
}

// LookupRequest always carries an explicit as-of instant, so "current" lookups
// are merely historical lookups whose caller intentionally chose the current UTC instant.
type LookupRequest struct {
	Pair     Pair
	AsOf     domain.UTCInstant
	RateType RateType
}

func (r LookupRequest) Validate() error {
	if err := r.Pair.Validate(); err != nil {
		return err
	}
	if err := r.AsOf.Validate(); err != nil {
		return fmt.Errorf("as_of: %w", err)
	}
	return r.RateType.Validate()
}

// Provider is the Task-089a runtime port. Task 089b supplies storage/cache and
// reference adapters. The port must return an immutable fact, never a naked number.
type Provider interface {
	ID() SourceID
	Lookup(context.Context, LookupRequest) (RateFact, error)
}

// ValidateLookupResult is the host-side contract check for a provider response.
func ValidateLookupResult(source SourceID, request LookupRequest, fact RateFact) error {
	if err := source.Validate(); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	if err := fact.Validate(); err != nil {
		return err
	}
	if fact.Source() != source || fact.Pair() != request.Pair || fact.RateType() != request.RateType {
		return ErrSourceResultMismatch
	}
	if fact.EffectiveAt().Time().After(request.AsOf.Time()) {
		return ErrSourceResultMismatch
	}
	return nil
}

// SourcePrecedence is a deterministic ordered source policy. It does not apply
// freshness limits; Task 089b owns staleness and reconciliation policy.
type SourcePrecedence struct{ ordered []SourceID }

func NewSourcePrecedence(values ...SourceID) (SourcePrecedence, error) {
	if len(values) == 0 {
		return SourcePrecedence{}, errors.New("at least one FX source is required")
	}
	seen := make(map[SourceID]struct{}, len(values))
	copied := make([]SourceID, len(values))
	for i, value := range values {
		if err := value.Validate(); err != nil {
			return SourcePrecedence{}, err
		}
		if _, duplicate := seen[value]; duplicate {
			return SourcePrecedence{}, errors.New("FX source precedence contains duplicate source")
		}
		seen[value] = struct{}{}
		copied[i] = value
	}
	return SourcePrecedence{ordered: copied}, nil
}

func (p SourcePrecedence) Sources() []SourceID { return append([]SourceID(nil), p.ordered...) }
func (p SourcePrecedence) Validate() error {
	_, err := NewSourcePrecedence(p.ordered...)
	return err
}

// Select chooses the highest-precedence latest applicable fact. It never
// inverts a pair, triangulates, or evaluates staleness.
func (p SourcePrecedence) Select(request LookupRequest, facts []RateFact) (RateFact, error) {
	if err := p.Validate(); err != nil {
		return RateFact{}, err
	}
	if err := request.Validate(); err != nil {
		return RateFact{}, err
	}
	rank := make(map[SourceID]int, len(p.ordered))
	for i, source := range p.ordered {
		rank[source] = i
	}
	candidates := make([]RateFact, 0, len(facts))
	for _, fact := range facts {
		if err := fact.Validate(); err != nil {
			return RateFact{}, err
		}
		if fact.Pair() != request.Pair || fact.RateType() != request.RateType || fact.EffectiveAt().Time().After(request.AsOf.Time()) {
			continue
		}
		if _, ok := rank[fact.Source()]; !ok {
			continue
		}
		candidates = append(candidates, fact)
	}
	if len(candidates) == 0 {
		return RateFact{}, ErrRateMissing
	}
	sort.Slice(candidates, func(i, j int) bool {
		ri, rj := rank[candidates[i].Source()], rank[candidates[j].Source()]
		if ri != rj {
			return ri < rj
		}
		ei, ej := candidates[i].EffectiveAt().Time(), candidates[j].EffectiveAt().Time()
		if !ei.Equal(ej) {
			return ei.After(ej)
		}
		oi, oj := candidates[i].ObservedAt().Time(), candidates[j].ObservedAt().Time()
		if !oi.Equal(oj) {
			return oi.After(oj)
		}
		return candidates[i].ID() < candidates[j].ID()
	})
	return candidates[0], nil
}

// RoundingMode is intentionally explicit; conversion code must never rely on
// language/runtime default rounding.
type RoundingMode string

const (
	RoundHalfEven   RoundingMode = "half_even"
	RoundHalfUp     RoundingMode = "half_up"
	RoundTowardZero RoundingMode = "toward_zero"
)

type RoundingPolicy struct{ mode RoundingMode }

func NewRoundingPolicy(mode RoundingMode) (RoundingPolicy, error) {
	switch mode {
	case RoundHalfEven, RoundHalfUp, RoundTowardZero:
		return RoundingPolicy{mode: mode}, nil
	default:
		return RoundingPolicy{}, errors.New("unsupported FX rounding mode")
	}
}

func (p RoundingPolicy) Mode() RoundingMode { return p.mode }
func (p RoundingPolicy) Validate() error {
	_, err := NewRoundingPolicy(p.mode)
	return err
}
func (p RoundingPolicy) Stage() string { return "final_amount_only" }

// TriangulationMode forbids arbitrary graph search. Stage 089a permits either a
// direct pair or at most one explicit pivot; it does not calculate a converted amount.
type TriangulationMode string

const (
	TriangulationDirectOnly  TriangulationMode = "direct_only"
	TriangulationSinglePivot TriangulationMode = "single_pivot"
)

type TriangulationPolicy struct {
	mode  TriangulationMode
	pivot domain.Currency
}

func NewTriangulationPolicy(mode TriangulationMode, pivot domain.Currency) (TriangulationPolicy, error) {
	switch mode {
	case TriangulationDirectOnly:
		if pivot != "" {
			return TriangulationPolicy{}, errors.New("direct-only triangulation cannot declare a pivot")
		}
		return TriangulationPolicy{mode: mode}, nil
	case TriangulationSinglePivot:
		if err := pivot.Validate(); err != nil {
			return TriangulationPolicy{}, errors.New("single-pivot triangulation requires a canonical pivot currency")
		}
		return TriangulationPolicy{mode: mode, pivot: pivot}, nil
	default:
		return TriangulationPolicy{}, errors.New("unsupported triangulation mode")
	}
}

func (p TriangulationPolicy) Mode() TriangulationMode { return p.mode }
func (p TriangulationPolicy) Pivot() domain.Currency  { return p.pivot }
func (p TriangulationPolicy) Validate() error {
	_, err := NewTriangulationPolicy(p.mode, p.pivot)
	return err
}

func (p TriangulationPolicy) Route(base, quote domain.Currency) ([]Pair, error) {
	direct, err := NewPair(base, quote)
	if err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if p.mode == TriangulationDirectOnly || base == p.pivot || quote == p.pivot {
		return []Pair{direct}, nil
	}
	first, err := NewPair(base, p.pivot)
	if err != nil {
		return nil, err
	}
	second, err := NewPair(p.pivot, quote)
	if err != nil {
		return nil, err
	}
	return []Pair{first, second}, nil
}

// ConversionSnapshot records every fact and policy required to reproduce a
// future Task-089b derivation. Stage 089a validates provenance shape only and
// deliberately does not expose an arithmetic conversion function.
type ConversionSnapshot struct {
	SchemaVersion        uint16            `json:"schema_version"`
	SourceAmount         domain.Money      `json:"source_amount"`
	TargetAmount         domain.Money      `json:"target_amount"`
	RateFacts            []RateFact        `json:"rate_facts"`
	RoundingMode         RoundingMode      `json:"rounding_mode"`
	RoundingStage        string            `json:"rounding_stage"`
	TriangulationMode    TriangulationMode `json:"triangulation_mode"`
	PivotCurrency        domain.Currency   `json:"pivot_currency,omitempty"`
	TargetMinorUnitScale uint8             `json:"target_minor_unit_scale"`
	DerivedAt            domain.UTCInstant `json:"derived_at"`
}

func NewConversionSnapshot(source, target domain.Money, facts []RateFact, rounding RoundingPolicy, triangulation TriangulationPolicy, targetMinorUnitScale uint8, derivedAt domain.UTCInstant) (ConversionSnapshot, error) {
	value := ConversionSnapshot{
		SchemaVersion: SchemaVersion, SourceAmount: source, TargetAmount: target,
		RateFacts: append([]RateFact(nil), facts...), RoundingMode: rounding.Mode(),
		RoundingStage: rounding.Stage(), TriangulationMode: triangulation.Mode(),
		PivotCurrency: triangulation.Pivot(), TargetMinorUnitScale: targetMinorUnitScale,
		DerivedAt: derivedAt,
	}
	if err := value.Validate(); err != nil {
		return ConversionSnapshot{}, err
	}
	return value, nil
}

func (s ConversionSnapshot) Validate() error {
	if s.SchemaVersion != SchemaVersion {
		return errors.New("unsupported FX conversion snapshot schema version")
	}
	if err := s.SourceAmount.Validate(); err != nil {
		return fmt.Errorf("source amount: %w", err)
	}
	if err := s.TargetAmount.Validate(); err != nil {
		return fmt.Errorf("target amount: %w", err)
	}
	if s.SourceAmount.Currency() == s.TargetAmount.Currency() {
		return errors.New("FX conversion snapshot requires different source and target currencies")
	}
	if s.TargetMinorUnitScale > domain.MaxDecimalScale {
		return errors.New("target minor-unit scale exceeds exact decimal limit")
	}
	if err := s.DerivedAt.Validate(); err != nil {
		return fmt.Errorf("derived_at: %w", err)
	}
	rounding, err := NewRoundingPolicy(s.RoundingMode)
	if err != nil || s.RoundingStage != rounding.Stage() {
		return errors.New("invalid FX rounding snapshot policy")
	}
	triangulation, err := NewTriangulationPolicy(s.TriangulationMode, s.PivotCurrency)
	if err != nil {
		return err
	}
	route, err := triangulation.Route(s.SourceAmount.Currency(), s.TargetAmount.Currency())
	if err != nil {
		return err
	}
	if len(s.RateFacts) == 0 || len(s.RateFacts) > MaxSnapshotRateFacts || len(s.RateFacts) != len(route) {
		return errors.New("FX conversion snapshot rate facts do not match triangulation route")
	}
	seen := make(map[string]struct{}, len(s.RateFacts))
	for i, fact := range s.RateFacts {
		if err := fact.Validate(); err != nil {
			return err
		}
		if fact.Pair() != route[i] {
			return errors.New("FX conversion snapshot contains a rate fact for the wrong route leg")
		}
		if fact.ObservedAt().Time().After(s.DerivedAt.Time()) || fact.EffectiveAt().Time().After(s.DerivedAt.Time()) {
			return errors.New("FX conversion snapshot cannot use a future rate fact")
		}
		if _, duplicate := seen[fact.ID()]; duplicate {
			return errors.New("FX conversion snapshot contains duplicate rate fact id")
		}
		seen[fact.ID()] = struct{}{}
	}
	return nil
}

func (s ConversionSnapshot) MarshalJSON() ([]byte, error) {
	type alias ConversionSnapshot
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(alias(s))
}

func containsCredentialLikeText(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"authorization", "bearer ", "password", "client_secret", "refresh_token", "access_token", "api_key"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
