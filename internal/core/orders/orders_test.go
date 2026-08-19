package orders

import (
	"errors"
	"testing"
	"time"
)

const (
	orgID   = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	wsID    = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
	orderID = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0601"
	itemID  = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0602"
	offerID = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102"
)

func fixtureCreate(t *testing.T) CreateOrder {
	t.Helper()
	rub, _ := NewCurrency("RUB")
	qty, _ := NewQuantity(mustDecimal(t, "2.5"), mustUnit(t, "PCS"))
	unit, _ := NewMoney(4000, rub)
	sub, _ := NewMoney(10000, rub)
	disc, _ := NewMoney(1000, rub)
	tax, _ := NewMoney(1800, rub)
	line, _ := NewMoney(10800, rub)
	shipping, _ := NewMoney(500, rub)
	return CreateOrder{ID: OrderID(orderID), Number: "ORD-2026-0001", Currency: rub, ShippingTotal: shipping, PlacedAt: time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC), Items: []CreateItem{{ID: OrderItemID(itemID), OfferID: OfferID(offerID), Position: 1, SKU: "TSHIRT-001-M", Quantity: qty, UnitPrice: unit, Subtotal: sub, DiscountTotal: disc, TaxTotal: tax, LineTotal: line, Tax: TaxSnapshot{Jurisdiction: "RU", Category: "standard", Rate: mustDecimal(t, "0.2"), PriceIncludesTax: false}}}}
}
func mustDecimal(t *testing.T, v string) Decimal {
	t.Helper()
	d, e := ParseDecimal(v)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func mustUnit(t *testing.T, v string) UnitCode {
	t.Helper()
	u, e := NewUnitCode(v)
	if e != nil {
		t.Fatal(e)
	}
	return u
}

func TestBuildCreateCalculatesExactTotals(t *testing.T) {
	scope, _ := ParseScope(orgID, wsID)
	at := time.Date(2026, 8, 9, 9, 1, 0, 0, time.UTC)
	o, err := BuildCreate(fixtureCreate(t), scope, at)
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusPending || o.Version != 1 || o.Subtotal.MinorUnits() != 10000 || o.DiscountTotal.MinorUnits() != 1000 || o.TaxTotal.MinorUnits() != 1800 || o.GrandTotal.MinorUnits() != 11300 {
		t.Fatalf("unexpected order: %#v", o)
	}
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestIncludedTaxDoesNotDoubleCountLineTotal(t *testing.T) {
	c := fixtureCreate(t)
	rub := c.Currency
	c.Items[0].Tax.PriceIncludesTax = true
	c.Items[0].TaxTotal, _ = NewMoney(1500, rub)
	c.Items[0].LineTotal, _ = NewMoney(9000, rub)
	scope, _ := ParseScope(orgID, wsID)
	o, e := BuildCreate(c, scope, time.Date(2026, 8, 9, 9, 1, 0, 0, time.UTC))
	if e != nil {
		t.Fatal(e)
	}
	if o.GrandTotal.MinorUnits() != 9500 {
		t.Fatalf("grand=%d", o.GrandTotal.MinorUnits())
	}
}
func TestRejectsInvalidLineAndTax(t *testing.T) {
	c := fixtureCreate(t)
	c.Items[0].LineTotal, _ = NewMoney(9999, c.Currency)
	if c.Validate() == nil {
		t.Fatal("expected invalid line total")
	}
	c = fixtureCreate(t)
	c.Items[0].Tax.Rate = mustDecimal(t, "1.01")
	if c.Validate() == nil {
		t.Fatal("expected invalid tax rate")
	}
}
func TestOrderLifecycle(t *testing.T) {
	valid := [][2]Status{{StatusPending, StatusConfirmed}, {StatusPending, StatusCancelled}, {StatusConfirmed, StatusProcessing}, {StatusConfirmed, StatusCancelled}, {StatusProcessing, StatusFulfilled}, {StatusProcessing, StatusCancelled}}
	for _, p := range valid {
		if err := ValidateTransition(p[0], p[1]); err != nil {
			t.Fatalf("%s -> %s: %v", p[0], p[1], err)
		}
	}
	for _, p := range [][2]Status{{StatusPending, StatusFulfilled}, {StatusFulfilled, StatusCancelled}, {StatusCancelled, StatusConfirmed}, {StatusConfirmed, StatusFulfilled}} {
		if !errors.Is(ValidateTransition(p[0], p[1]), ErrInvalidState) {
			t.Fatalf("expected invalid %s -> %s", p[0], p[1])
		}
	}
}
func TestDecimalAndMoneyAreExact(t *testing.T) {
	d := mustDecimal(t, "0.100000001")
	if d.String() != "0.100000001" || d.Scale() != 9 {
		t.Fatalf("decimal=%s scale=%d", d, d.Scale())
	}
	if _, e := ParseDecimal("0.1000000001"); e == nil {
		t.Fatal("expected scale rejection")
	}
	rub, _ := NewCurrency("RUB")
	if _, e := NewMoney(-1, rub); e == nil {
		t.Fatal("negative money accepted")
	}
}
func TestNoProviderStatuses(t *testing.T) {
	for _, v := range []Status{"awaiting_packaging", "delivering", "wb_new", "ozon_processing"} {
		if v.Valid() {
			t.Fatalf("provider status accepted: %s", v)
		}
	}
}
