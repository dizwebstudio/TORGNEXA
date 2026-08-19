package inventory

import (
	"testing"
	"time"
)

func q(t *testing.T, v string) Quantity {
	t.Helper()
	d, e := ParseDecimal(v)
	if e != nil {
		t.Fatal(e)
	}
	u, _ := NewUnitCode("EA")
	r, e := NewQuantity(d, u)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestPositionAvailableAndInvariant(t *testing.T) {
	p := Position{ID: PositionID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0301"), OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", OfferID: OfferID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102"), WarehouseID: WarehouseID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0302"), OnHand: q(t, "10"), Reserved: q(t, "3.5"), Version: 1, CreatedAt: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)}
	if e := p.Validate(); e != nil {
		t.Fatal(e)
	}
	a, e := p.Available()
	if e != nil || a.Value.String() != "6.5" {
		t.Fatalf("available=%v err=%v", a, e)
	}
	p.Reserved = q(t, "10.1")
	if p.Validate() == nil {
		t.Fatal("reserved > onhand accepted")
	}
}
func TestQuantityChangeRejectsNegative(t *testing.T) {
	c := ChangeQuantity{ID: PositionID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0301"), ExpectedVersion: 1, Quantity: q(t, "1"), Reason: "order.reserve"}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	c.Quantity = q(t, "-1")
	if c.Validate() == nil {
		t.Fatal("negative accepted")
	}
}
