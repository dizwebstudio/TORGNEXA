package promotions

import (
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"testing"
)

func money(t *testing.T, n int64) domain.Money {
	c, _ := domain.NewCurrency("RUB")
	m, e := domain.NewMoney(n, c)
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func scope(t *testing.T) tenancy.Scope {
	s, e := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestPreviewBlocksFloorAndMargin(t *testing.T) {
	p := GuardPolicy{"g", money(t, 1000), 2000, 10, 2}
	items := []Candidate{{"A", money(t, 1500), money(t, 900), money(t, 700)}}
	got, e := AuthorizeMassWrite(scope(t), p, items, true)
	if !errors.Is(e, ErrGuardViolation) || len(got.Violations) != 1 || got.Violations[0].Code != "floor_price" {
		t.Fatalf("got=%+v err=%v", got, e)
	}
}
func TestMassWritePreviewsAndRequiresApproval(t *testing.T) {
	p := GuardPolicy{"g", money(t, 1000), 1000, 10, 2}
	items := []Candidate{{"B", money(t, 1500), money(t, 1400), money(t, 1000)}, {"A", money(t, 1500), money(t, 1400), money(t, 1000)}}
	got, e := AuthorizeMassWrite(scope(t), p, items, false)
	if !errors.Is(e, ErrApprovalRequired) || !got.ApprovalRequired || len(got.AffectedSKUs) != 2 || got.AffectedSKUs[0] != "A" {
		t.Fatalf("got=%+v err=%v", got, e)
	}
	if _, e := AuthorizeMassWrite(scope(t), p, items, true); e != nil {
		t.Fatal(e)
	}
}

func TestParticipationValidation(t *testing.T) {
	p := Participation{PromotionID: "promo-1", SKU: "sku-1", Proposed: money(t, 9900), Version: 1}
	if err := p.Validate(); err != nil {
		t.Fatalf("valid participation rejected: %v", err)
	}
	p.SKU = ""
	if err := p.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid participation error = %v", err)
	}
}
