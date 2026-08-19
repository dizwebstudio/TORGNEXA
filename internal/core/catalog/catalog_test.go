package catalog

import (
	"testing"
	"time"
)

const (
	orgID     = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	wsID      = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
	productID = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101"
	offerID   = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102"
)

func TestProductOfferValidation(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	product := Product{ID: ProductID(productID), OrganizationID: orgID, WorkspaceID: wsID, Code: "TSHIRT-001", Title: "Synthetic T-shirt", Description: "Catalog test item", Status: StatusDraft, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := product.Validate(); err != nil {
		t.Fatalf("product: %v", err)
	}
	offer := Offer{ID: OfferID(offerID), OrganizationID: orgID, WorkspaceID: wsID, ProductID: product.ID, SKU: "TSHIRT-001-M", GTIN: "4006381333931", Status: StatusDraft, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := offer.Validate(); err != nil {
		t.Fatalf("offer: %v", err)
	}
	offer.GTIN = "4006381333932"
	if err := offer.Validate(); err == nil {
		t.Fatal("invalid GTIN check digit accepted")
	}
}

func TestLifecycleIsForwardOnly(t *testing.T) {
	for _, tc := range []struct {
		from, to Status
		ok       bool
	}{
		{StatusDraft, StatusActive, true}, {StatusDraft, StatusArchived, true}, {StatusActive, StatusArchived, true},
		{StatusActive, StatusDraft, false}, {StatusArchived, StatusActive, false}, {StatusDraft, StatusDraft, false},
	} {
		err := ValidateProductTransition(tc.from, tc.to)
		if tc.ok && err != nil {
			t.Errorf("%s -> %s rejected: %v", tc.from, tc.to, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s -> %s accepted", tc.from, tc.to)
		}
	}
}

func TestCommandsAndMutationRejectUnsafeValues(t *testing.T) {
	if err := (CreateProduct{ID: ProductID(productID), Code: "bad code", Title: "x"}).Validate(); err == nil {
		t.Fatal("unsafe code accepted")
	}
	if err := (CreateOffer{ID: OfferID(offerID), ProductID: ProductID(productID), SKU: "SKU-1", GTIN: "123"}).Validate(); err == nil {
		t.Fatal("invalid GTIN accepted")
	}
	mutation := Mutation{EventID: "evt.catalog.1", OccurredAt: time.Date(2026, 8, 9, 10, 0, 0, 0, time.FixedZone("x", 3600)), Source: "api"}
	if err := mutation.Validate(); err == nil {
		t.Fatal("non-UTC mutation accepted")
	}
	mutation.OccurredAt = mutation.OccurredAt.UTC()
	if err := mutation.Validate(); err != nil {
		t.Fatalf("valid mutation rejected: %v", err)
	}
}

func TestArchivedAggregatesValidateAsHistoricalRecords(t *testing.T) {
	created := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	updated := created.Add(time.Hour)
	product := Product{ID: ProductID(productID), OrganizationID: orgID, WorkspaceID: wsID, Code: "P-1", Title: "Archived", Status: StatusArchived, Version: 3, CreatedAt: created, UpdatedAt: updated}
	if err := product.Validate(); err != nil {
		t.Fatal(err)
	}
}
