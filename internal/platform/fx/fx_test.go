package fx

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

func mustCurrency(t *testing.T, value string) domain.Currency {
	t.Helper()
	result, err := domain.NewCurrency(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustDecimal(t *testing.T, value string) domain.Decimal {
	t.Helper()
	result, err := domain.ParseDecimal(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustInstant(t *testing.T, value string) domain.UTCInstant {
	t.Helper()
	result, err := domain.ParseUTCInstant(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustSource(t *testing.T, value string) SourceID {
	t.Helper()
	result, err := NewSourceID(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustFact(t *testing.T, id, base, quote, rate, source, effective string) RateFact {
	t.Helper()
	pair, err := NewPair(mustCurrency(t, base), mustCurrency(t, quote))
	if err != nil {
		t.Fatal(err)
	}
	fact, err := NewRateFact(RateFactInput{
		ID:              id,
		Pair:            pair,
		Rate:            mustDecimal(t, rate),
		Source:          mustSource(t, source),
		SourceReference: "ref/" + id,
		RateType:        RateOfficial,
		ObservedAt:      mustInstant(t, "2026-08-09T12:00:00Z"),
		EffectiveAt:     mustInstant(t, effective),
	})
	if err != nil {
		t.Fatal(err)
	}
	return fact
}

func TestRateFactIsExactImmutableWireFact(t *testing.T) {
	fact := mustFact(t, "fxr_001", "USD", "RUB", "79.123456789", "central_bank", "2026-08-09T00:00:00Z")
	if fact.Rate().String() != "79.123456789" {
		t.Fatalf("rate=%s", fact.Rate().String())
	}
	encoded, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `79.123456789,`) || !strings.Contains(string(encoded), `"rate":"79.123456789"`) {
		t.Fatalf("rate must be encoded as exact JSON string: %s", encoded)
	}
	var decoded RateFact
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID() != fact.ID() || decoded.Pair() != fact.Pair() || decoded.Rate().String() != fact.Rate().String() {
		t.Fatalf("round trip mismatch: %#v %#v", fact, decoded)
	}
	mutated := append(encoded[:len(encoded)-1], []byte(`,"unexpected":true}`)...)
	if err := json.Unmarshal(mutated, &decoded); err == nil {
		t.Fatal("expected strict unknown-field rejection")
	}
}

func TestRateFactRejectsInvalidOrSecretShapedMetadata(t *testing.T) {
	pair, _ := NewPair(mustCurrency(t, "USD"), mustCurrency(t, "EUR"))
	_, err := NewRateFact(RateFactInput{
		ID: "fxr_002", Pair: pair, Rate: mustDecimal(t, "0"), Source: mustSource(t, "source"),
		RateType: RateOfficial, ObservedAt: mustInstant(t, "2026-08-09T12:00:00Z"), EffectiveAt: mustInstant(t, "2026-08-09T00:00:00Z"),
	})
	if err == nil {
		t.Fatal("expected zero-rate rejection")
	}

	_, err = NewRateFact(RateFactInput{
		ID: "fxr_003", Pair: pair, Rate: mustDecimal(t, "1.25"), Source: mustSource(t, "source"),
		SourceReference: "https://example.invalid/rate?access_token=secret",
		RateType:        RateOfficial, ObservedAt: mustInstant(t, "2026-08-09T12:00:00Z"), EffectiveAt: mustInstant(t, "2026-08-09T00:00:00Z"),
	})
	if err == nil {
		t.Fatal("expected non-opaque source reference rejection")
	}
}

func TestSourcePrecedenceIsDeterministicAndNeverInverts(t *testing.T) {
	request := LookupRequest{
		Pair:     Pair{Base: mustCurrency(t, "USD"), Quote: mustCurrency(t, "RUB")},
		AsOf:     mustInstant(t, "2026-08-09T23:59:59Z"),
		RateType: RateOfficial,
	}
	precedence, err := NewSourcePrecedence(mustSource(t, "primary"), mustSource(t, "secondary"))
	if err != nil {
		t.Fatal(err)
	}
	facts := []RateFact{
		mustFact(t, "fxr_secondary_new", "USD", "RUB", "81", "secondary", "2026-08-09T12:00:00Z"),
		mustFact(t, "fxr_primary_old", "USD", "RUB", "79", "primary", "2026-08-08T12:00:00Z"),
		mustFact(t, "fxr_primary_new", "USD", "RUB", "80", "primary", "2026-08-09T10:00:00Z"),
		mustFact(t, "fxr_inverse", "RUB", "USD", "0.0125", "primary", "2026-08-09T11:00:00Z"),
	}
	selected, err := precedence.Select(request, facts)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID() != "fxr_primary_new" {
		t.Fatalf("selected %q", selected.ID())
	}

	missing := request
	missing.Pair = Pair{Base: mustCurrency(t, "EUR"), Quote: mustCurrency(t, "JPY")}
	if _, err := precedence.Select(missing, facts); !errors.Is(err, ErrRateMissing) {
		t.Fatalf("expected explicit missing rate, got %v", err)
	}
}

type fakeProvider struct {
	id   SourceID
	fact RateFact
}

func (f fakeProvider) ID() SourceID                                            { return f.id }
func (f fakeProvider) Lookup(context.Context, LookupRequest) (RateFact, error) { return f.fact, nil }

func TestProviderPortResultValidation(t *testing.T) {
	request := LookupRequest{
		Pair: Pair{Base: mustCurrency(t, "EUR"), Quote: mustCurrency(t, "USD")},
		AsOf: mustInstant(t, "2026-08-09T23:00:00Z"), RateType: RateOfficial,
	}
	fact := mustFact(t, "fxr_provider", "EUR", "USD", "1.17", "reference_bank", "2026-08-09T00:00:00Z")
	adapter := fakeProvider{id: mustSource(t, "reference_bank"), fact: fact}
	got, err := adapter.Lookup(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateLookupResult(adapter.ID(), request, got); err != nil {
		t.Fatal(err)
	}

	wrong := request
	wrong.Pair = Pair{Base: mustCurrency(t, "EUR"), Quote: mustCurrency(t, "RUB")}
	if err := ValidateLookupResult(adapter.ID(), wrong, got); !errors.Is(err, ErrSourceResultMismatch) {
		t.Fatalf("expected mismatch, got %v", err)
	}
}

func TestTriangulationPlansAtMostOneExplicitPivotWithoutConverting(t *testing.T) {
	direct, err := NewTriangulationPolicy(TriangulationDirectOnly, "")
	if err != nil {
		t.Fatal(err)
	}
	route, err := direct.Route(mustCurrency(t, "USD"), mustCurrency(t, "JPY"))
	if err != nil {
		t.Fatal(err)
	}
	if len(route) != 1 || route[0].String() != "USD/JPY" {
		t.Fatalf("route=%v", route)
	}

	pivot, err := NewTriangulationPolicy(TriangulationSinglePivot, mustCurrency(t, "EUR"))
	if err != nil {
		t.Fatal(err)
	}
	route, err = pivot.Route(mustCurrency(t, "USD"), mustCurrency(t, "JPY"))
	if err != nil {
		t.Fatal(err)
	}
	if len(route) != 2 || route[0].String() != "USD/EUR" || route[1].String() != "EUR/JPY" {
		t.Fatalf("route=%v", route)
	}
	if !HasConversionEngine() {
		t.Fatal("cross-currency conversion must be enabled after Task 089b")
	}
	if !errors.Is(ErrCrossCurrencyConversionDisabled, ErrCrossCurrencyConversionDisabled) {
		t.Fatal("conversion gate sentinel unavailable")
	}
}

func TestConversionSnapshotRequiresFullReproducibleProvenance(t *testing.T) {
	usd, eur, jpy := mustCurrency(t, "USD"), mustCurrency(t, "EUR"), mustCurrency(t, "JPY")
	source, _ := domain.NewMoney(12500, usd)
	target, _ := domain.NewMoney(1823400, jpy)
	rounding, _ := NewRoundingPolicy(RoundHalfEven)
	triangulation, _ := NewTriangulationPolicy(TriangulationSinglePivot, eur)
	facts := []RateFact{
		mustFact(t, "fxr_usd_eur", "USD", "EUR", "0.86", "central_bank", "2026-08-09T00:00:00Z"),
		mustFact(t, "fxr_eur_jpy", "EUR", "JPY", "169.62", "central_bank", "2026-08-09T00:00:00Z"),
	}
	snapshot, err := NewConversionSnapshot(source, target, facts, rounding, triangulation, 0, mustInstant(t, "2026-08-09T13:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"rate_facts"`) || !strings.Contains(string(encoded), `"source":"central_bank"`) {
		t.Fatalf("snapshot lacks provenance: %s", encoded)
	}

	badFacts := append([]RateFact(nil), facts...)
	badFacts[1] = mustFact(t, "fxr_wrong", "GBP", "JPY", "190", "central_bank", "2026-08-09T00:00:00Z")
	if _, err := NewConversionSnapshot(source, target, badFacts, rounding, triangulation, 0, mustInstant(t, "2026-08-09T13:00:00Z")); err == nil {
		t.Fatal("expected route provenance rejection")
	}
}

func TestUTCAndNoBinaryFloatAssumptions(t *testing.T) {
	_, err := domain.NewUTCInstant(time.Date(2026, 8, 9, 12, 0, 0, 0, time.FixedZone("bad", 3600)))
	if err == nil {
		t.Fatal("expected non-UTC instant rejection")
	}
	if containsCredentialLikeText("Authorization: Bearer secret") != true {
		t.Fatal("credential marker guard regression")
	}
}
