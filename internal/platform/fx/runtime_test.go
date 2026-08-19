package fx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

type countingProvider struct {
	id    SourceID
	fact  RateFact
	calls int
}

func (p *countingProvider) ID() SourceID { return p.id }
func (p *countingProvider) Lookup(context.Context, LookupRequest) (RateFact, error) {
	p.calls++
	return p.fact, nil
}

func TestResolverPersistsBeforeCacheAndFailsClosedOnStale(t *testing.T) {
	store := NewMemoryStore()
	fact := mustFact(t, "cbr:2026-08-10:USD:RUB", "USD", "RUB", "80.5", "cbr", "2026-08-10T00:00:00Z")
	p := &countingProvider{id: mustSource(t, "cbr"), fact: fact}
	prec, _ := NewSourcePrecedence(mustSource(t, "cbr"))
	resolver, err := NewResolver(store, []Provider{p}, prec, map[SourceID]FreshnessPolicy{mustSource(t, "cbr"): {MaxEffectiveAge: 96 * time.Hour}}, NewMemoryCache(time.Hour, 10), func() time.Time { return time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	pair, _ := NewPair(mustCurrency(t, "USD"), mustCurrency(t, "RUB"))
	req := LookupRequest{Pair: pair, AsOf: mustInstant(t, "2026-08-12T01:00:00Z"), RateType: RateOfficial}
	got, ev, err := resolver.Resolve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != fact.ID() || p.calls != 1 || ev.SelectedFactID != fact.ID() {
		t.Fatalf("got=%s calls=%d", got.ID(), p.calls)
	}
	if _, _, err = resolver.Resolve(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 {
		t.Fatalf("cache did not reuse persisted fact: %d", p.calls)
	}
	staleReq := req
	staleReq.AsOf = mustInstant(t, "2026-08-20T01:00:00Z")
	if _, _, err = resolver.Resolve(context.Background(), staleReq); !errors.Is(err, ErrRateStale) {
		t.Fatalf("expected stale failure, got %v", err)
	}
}

func TestExactConversionAndSnapshotVerification(t *testing.T) {
	store := NewMemoryStore()
	fact := mustFact(t, "cbr:2026-08-11:USD:RUB", "USD", "RUB", "80.5", "cbr", "2026-08-11T00:00:00Z")
	if err := store.AppendFact(context.Background(), fact); err != nil {
		t.Fatal(err)
	}
	prec, _ := NewSourcePrecedence(mustSource(t, "cbr"))
	resolver, _ := NewResolver(store, nil, prec, map[SourceID]FreshnessPolicy{mustSource(t, "cbr"): {MaxEffectiveAge: 96 * time.Hour}}, nil, func() time.Time { return time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC) })
	usd, _ := domain.NewCurrency("USD")
	rub, _ := domain.NewCurrency("RUB")
	money, _ := domain.NewMoney(12345, usd)
	rounding, _ := NewRoundingPolicy(RoundHalfEven)
	tri, _ := NewTriangulationPolicy(TriangulationDirectOnly, "")
	rec, err := resolver.Convert(context.Background(), ConversionRequest{ID: "fxconv_001", Source: money, SourceMinorUnitScale: 2, TargetCurrency: rub, TargetMinorUnitScale: 2, AsOf: mustInstant(t, "2026-08-12T01:00:00Z"), RateType: RateOfficial, Rounding: rounding, Triangulation: tri})
	if err != nil {
		t.Fatal(err)
	}
	// 123.45 * 80.5 = 9937.725 RUB => 993772 kopecks with half-even (tie to even).
	if rec.Snapshot.TargetAmount.MinorUnits() != 993772 {
		t.Fatalf("minor=%d", rec.Snapshot.TargetAmount.MinorUnits())
	}
	if err := VerifySnapshotArithmetic(rec.Snapshot, 2); err != nil {
		t.Fatal(err)
	}
	bad := rec.Snapshot
	wrong, _ := domain.NewMoney(993773, rub)
	bad.TargetAmount = wrong
	if !errors.Is(VerifySnapshotArithmetic(bad, 2), ErrSnapshotArithmetic) {
		t.Fatal("tampered snapshot accepted")
	}
}

func TestRoundingModesAreExplicitForNegativeValues(t *testing.T) {
	f := mustFact(t, "fxr_half", "USD", "RUB", "0.5", "cbr", "2026-08-11T00:00:00Z")
	if got, _ := convertMinor(1, 0, 0, []RateFact{f}, RoundHalfEven); got != 0 {
		t.Fatalf("half-even=%d", got)
	}
	if got, _ := convertMinor(1, 0, 0, []RateFact{f}, RoundHalfUp); got != 1 {
		t.Fatalf("half-up=%d", got)
	}
	if got, _ := convertMinor(-1, 0, 0, []RateFact{f}, RoundHalfUp); got != -1 {
		t.Fatalf("negative half-up=%d", got)
	}
	if got, _ := convertMinor(-3, 0, 0, []RateFact{f}, RoundHalfEven); got != -2 {
		t.Fatalf("negative half-even=%d", got)
	}
}
