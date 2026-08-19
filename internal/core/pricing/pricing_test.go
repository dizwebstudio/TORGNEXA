package pricing

import (
	"testing"
	"time"
)

func TestPriceValidationAndMoneySafety(t *testing.T) {
	rub, _ := NewCurrency("RUB")
	amount, _ := NewMoney(12345, rub)
	p := Price{ID: PriceID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0201"), OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", OfferID: OfferID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102"), Kind: KindRegular, Amount: amount, Version: 1, CreatedAt: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	negative, _ := NewMoney(-1, rub)
	p.Amount = negative
	if p.Validate() == nil {
		t.Fatal("negative canonical price accepted")
	}
}
func TestMutationRequiresAuditAndEventIntent(t *testing.T) {
	m := Mutation{EventID: "evt.price.1", AuditID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0999", ActorID: "user-1", Source: "api", CorrelationID: "request-1", OccurredAt: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	m.AuditID = ""
	if m.Validate() == nil {
		t.Fatal("missing audit id accepted")
	}
}
