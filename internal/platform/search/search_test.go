package search

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const (
	testProductID = "018f0000-0000-7000-8000-000000000101"
	testOrderID   = "018f0000-0000-7000-8000-000000000201"
)

func TestProductQueryValidation(t *testing.T) {
	valid := []ProductQuery{
		{Limit: 50},
		{Text: "Drill 18V", Status: "active", Limit: 100},
		{Text: "SKU-42", Status: "archived", Limit: 1},
	}
	for _, query := range valid {
		if err := query.Validate(); err != nil {
			t.Fatalf("valid query rejected: %#v: %v", query, err)
		}
	}
	invalid := []ProductQuery{
		{Text: " leading", Limit: 50},
		{Text: "bad\nquery", Limit: 50},
		{Text: strings.Repeat("x", MaxQueryRunes+1), Limit: 50},
		{Status: "deleted", Limit: 50},
		{Limit: 0},
		{Limit: MaxPageSize + 1},
		{Limit: 50, Cursor: "bad!cursor"},
		{Limit: 50, Cursor: "v2.opaque"},
		{Limit: 50, Cursor: "v1."},
	}
	for _, query := range invalid {
		if err := query.Validate(); err == nil {
			t.Fatalf("invalid query accepted: %#v", query)
		}
	}
}

func TestOrderQueryRequiresUTCAndOrderedWindow(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	if err := (OrderQuery{Text: "ORD-42", Status: "processing", PlacedFrom: &from, PlacedTo: &to, Limit: 25}).Validate(); err != nil {
		t.Fatalf("valid order query: %v", err)
	}
	badZone := time.Date(2026, 8, 1, 0, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	cases := []OrderQuery{
		{Limit: 50, Status: "unknown"},
		{Limit: 50, PlacedFrom: &badZone},
		{Limit: 50, PlacedFrom: &to, PlacedTo: &from},
	}
	for _, query := range cases {
		if err := query.Validate(); err == nil {
			t.Fatalf("invalid order query accepted: %#v", query)
		}
	}
}

func TestCursorIsOpaqueBoundToQueryFingerprint(t *testing.T) {
	query := ProductQuery{Text: "drill", Status: "active", Limit: 10}
	fingerprint := ProductFingerprint(query)
	updatedAt := time.Date(2026, 8, 10, 9, 30, 0, 123, time.UTC)
	raw, err := NewCursor(1, updatedAt, testProductID, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := ParseCursor(raw, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if cursor.Priority != 1 || cursor.ID != testProductID || !cursor.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("cursor mismatch: %#v", cursor)
	}
	changed := query
	changed.Status = "archived"
	if _, err := ParseCursor(raw, ProductFingerprint(changed)); err == nil {
		t.Fatal("cursor reused across a changed query")
	}
}

func TestCursorRejectsTrailingJSON(t *testing.T) {
	query := ProductQuery{Text: "drill", Status: "active", Limit: 10}
	fingerprint := ProductFingerprint(query)
	raw, err := NewCursor(1, time.Date(2026, 8, 10, 9, 30, 0, 0, time.UTC), testProductID, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, "v1."))
	if err != nil {
		t.Fatal(err)
	}
	malformed := "v1." + base64.RawURLEncoding.EncodeToString(append(payload, []byte(`{"extra":true}`)...))
	if _, err := ParseCursor(malformed, fingerprint); err == nil {
		t.Fatal("cursor with trailing JSON accepted")
	}
}

func TestSearchPagesRejectInvalidProjectionValues(t *testing.T) {
	updatedAt := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	goodProduct := ProductHit{ID: testProductID, Code: "SKU-42", Title: "Cordless drill", Status: "active", UpdatedAt: updatedAt}
	if err := (ProductPage{Items: []ProductHit{goodProduct}}).Validate(); err != nil {
		t.Fatal(err)
	}
	badProduct := goodProduct
	badProduct.Title = " secret\n"
	if err := (ProductPage{Items: []ProductHit{badProduct}}).Validate(); err == nil {
		t.Fatal("invalid product projection accepted")
	}

	goodOrder := OrderHit{ID: testOrderID, OrderNumber: "ORD-42", Status: "confirmed", Currency: "RUB", GrandMinorUnits: 125000, PlacedAt: updatedAt.Add(-time.Hour), UpdatedAt: updatedAt}
	if err := (OrderPage{Items: []OrderHit{goodOrder}}).Validate(); err != nil {
		t.Fatal(err)
	}
	badOrder := goodOrder
	badOrder.Currency = "rub"
	if err := (OrderPage{Items: []OrderHit{badOrder}}).Validate(); err == nil {
		t.Fatal("invalid order projection accepted")
	}
}
