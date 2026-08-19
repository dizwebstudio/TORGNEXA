package paymentreconciliation

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/settlements"
	"testing"
	"time"
)

func mm(t *testing.T, n int64, c string) domain.Money {
	cc, _ := domain.NewCurrency(c)
	m, e := domain.NewMoney(n, cc)
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func TestClassifiesRequiredDifferenceFamiliesAndNoFX(t *testing.T) {
	sc, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	exp := []Expected{{"x1", "o1", "p1", mm(t, 10000, "RUB"), now}, {"x2", "o2", "p2", mm(t, 20000, "RUB"), now}}
	entries := []settlements.Entry{{ID: "e1", SourceSystem: "market-source", SourceAccountID: "a", SourceEntryRef: "r1", OrderID: "o1", Kind: settlements.KindSale, Amount: mm(t, 10000, "RUB"), OccurredAt: now, ImportedAt: now}, {ID: "e2", SourceSystem: "market-source", SourceAccountID: "a", SourceEntryRef: "fee", OrderID: "o1", FeeCode: "commission", Kind: settlements.KindFee, Amount: mm(t, -1000, "RUB"), OccurredAt: now, ImportedAt: now}, {ID: "e3", SourceSystem: "market-source", SourceAccountID: "a", SourceEntryRef: "late", OrderID: "o2", Kind: settlements.KindSale, Amount: mm(t, 20000, "RUB"), OccurredAt: now.Add(72 * time.Hour), ImportedAt: now}, {ID: "e4", SourceSystem: "market-source", SourceAccountID: "a", SourceEntryRef: "usd", OrderID: "o1", Kind: settlements.KindSale, Amount: mm(t, 100, "USD"), OccurredAt: now, ImportedAt: now}}
	r, e := Reconcile(sc, exp, entries, []Receipt{{"p1", "bank1", "o1", mm(t, 10000, "RUB"), now, false}, {"p2", "bank1", "o1", mm(t, 10000, "RUB"), now, false}}, Policy{24 * time.Hour, map[string]bool{"commission": true}}, now)
	if e != nil {
		t.Fatal(e)
	}
	kinds := map[DifferenceKind]bool{}
	for _, d := range r.Differences {
		kinds[d.Kind] = true
	}
	for _, k := range []DifferenceKind{DifferenceKnownFee, DifferenceTiming, DifferenceDisputed, DifferenceDuplicate} {
		if !kinds[k] {
			t.Fatalf("missing %s: %+v", k, r.Differences)
		}
	}
}

type fakeFXConverter struct {
	money domain.Money
	ref   string
	err   error
}

func (f fakeFXConverter) Convert(_ context.Context, _ string, _ domain.Money, _ domain.Currency, _ time.Time) (domain.Money, string, error) {
	return f.money, f.ref, f.err
}

func TestReconcileWithFXRequiresPersistedConversionReference(t *testing.T) {
	sc, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	exp := []Expected{{"x1", "o1", "p1", mm(t, 8050, "RUB"), now}}
	entries := []settlements.Entry{{ID: "e1", SourceSystem: "market-source", SourceAccountID: "a", SourceEntryRef: "usd", OrderID: "o1", Kind: settlements.KindSale, Amount: mm(t, 100, "USD"), OccurredAt: now, ImportedAt: now}}
	r, err := ReconcileWithFX(context.Background(), sc, exp, entries, nil, Policy{24 * time.Hour, map[string]bool{}}, now, fakeFXConverter{money: mm(t, 8050, "RUB"), ref: "fxconv:payrec:1"})
	if err != nil {
		t.Fatal(err)
	}
	if r.MatchedSettlement != 1 || len(r.FXConversionRefs) != 1 || r.FXConversionRefs[0] != "fxconv:payrec:1" {
		t.Fatalf("report=%+v", r)
	}
}

func TestReconcileWithFXFailsExplicitlyWhenRateUnavailable(t *testing.T) {
	sc, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	exp := []Expected{{"x1", "o1", "p1", mm(t, 8050, "RUB"), now}}
	entries := []settlements.Entry{{ID: "e1", SourceSystem: "market-source", SourceAccountID: "a", SourceEntryRef: "usd", OrderID: "o1", Kind: settlements.KindSale, Amount: mm(t, 100, "USD"), OccurredAt: now, ImportedAt: now}}
	_, err := ReconcileWithFX(context.Background(), sc, exp, entries, nil, Policy{24 * time.Hour, map[string]bool{}}, now, fakeFXConverter{err: errors.New("stale")})
	if !errors.Is(err, ErrFXUnavailable) {
		t.Fatalf("err=%v", err)
	}
}
