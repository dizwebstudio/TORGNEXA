package fx

import (
	"context"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

func TestFinancialConverterReturnsPersistedConversionReference(t *testing.T) {
	usd, _ := domain.NewCurrency("USD")
	rub, _ := domain.NewCurrency("RUB")
	pair, _ := NewPair(usd, rub)
	sourceID, _ := NewSourceID("cbr")
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	instant, _ := domain.NewUTCInstant(at)
	rate, _ := domain.ParseDecimal("80.5")
	fact, _ := NewRateFact(RateFactInput{ID: "cbr:test:usd-rub", Pair: pair, Rate: rate, Source: sourceID, RateType: RateOfficial, ObservedAt: instant, EffectiveAt: instant})
	store := NewMemoryStore()
	if err := store.AppendFact(context.Background(), fact); err != nil {
		t.Fatal(err)
	}
	precedence, _ := NewSourcePrecedence(sourceID)
	resolver, err := NewResolver(store, nil, precedence, map[SourceID]FreshnessPolicy{sourceID: {MaxEffectiveAge: 96 * time.Hour}}, nil, func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	scales, _ := NewMinorUnitRegistry(map[domain.Currency]uint8{usd: 2, rub: 2})
	rounding, _ := NewRoundingPolicy(RoundHalfEven)
	triangulation, _ := NewTriangulationPolicy(TriangulationDirectOnly, domain.Currency(""))
	converter, err := NewFinancialConverter(resolver, scales, RateOfficial, rounding, triangulation)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := domain.NewMoney(12345, usd)
	got, ref, err := converter.Convert(context.Background(), "finance:test:1", source, rub, at)
	if err != nil {
		t.Fatal(err)
	}
	if got.MinorUnits() != 993772 || got.Currency() != rub || ref != "finance:test:1" {
		t.Fatalf("got=%v %s", got.MinorUnits(), ref)
	}
	if _, ok := store.conversions[ref]; !ok {
		t.Fatal("conversion was not persisted")
	}
}
