package mcp

import (
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/catalogbulk"
	"github.com/torgnexa/torgnexa/internal/core/marketplacelisting"
)

func TestLocalCatalogBulkPreviewerIsTenantBoundAndDryRunOnly(t *testing.T) {
	identity := testIdentity(t, permissionCatalogBulkPreview)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	taxonomy := marketplacelisting.DemoTaxonomy("demo", "ru-RU", "RU", now)
	fingerprint, err := taxonomy.ComputeFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	draft := marketplacelisting.ListingDraft{
		ID: "listing-1", OrganizationID: identity.Tenant.OrganizationID().String(), WorkspaceID: identity.Tenant.WorkspaceID().String(), ProductID: "product-1", SKU: "SKU-1", CategoryCode: "demo.product", TaxonomyFingerprint: fingerprint, ProductVersion: 1,
		Attributes: map[string]marketplacelisting.AttributeValue{"color": {Value: "white"}},
		Content:    marketplacelisting.Content{Locale: "ru-RU", Title: "Товар"},
		Media:      []marketplacelisting.MediaRef{{ID: "media-1", Slot: "main", ReleasedObjectRef: "upl_media-1", Digest: strings.Repeat("a", 64), Format: "image/jpeg", Bytes: 1024, Width: 1000, Height: 1000, Released: true, Safe: true}},
	}
	input := CatalogBulkPreviewInput{
		Selection:   catalogbulk.SelectionSnapshot{FilterDigest: strings.Repeat("b", 64), Filter: "saved_filter", SKUs: []string{"SKU-1"}, Targets: []catalogbulk.ChannelTarget{{ChannelID: "demo", AccountID: "account-1", Label: "Demo", State: catalogbulk.CapabilityQualified, Capabilities: []string{"marketplace.listings.content.write"}, TaxonomyFingerprint: fingerprint, TaxonomyVersion: 1, MappingVersion: 1, ObservedAt: now, FreshUntil: now.Add(time.Hour)}}, SnapshotVersion: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		Projections: []catalogbulk.Projection{{SKU: "SKU-1", ProductID: "product-1", ChannelID: "demo", AccountID: "account-1", Draft: draft, Currency: "RUB", PriceMinorUnits: 10000, Stock: 2, Version: 1}},
		Changes:     []catalogbulk.Change{{Kind: "set", Field: "content.title", Value: "Обновлённый товар"}},
	}
	if err := input.Selection.Validate(now); err != nil {
		t.Fatalf("selection validation: %v", err)
	}
	for _, projection := range input.Projections {
		if err := projection.Validate(); err != nil {
			t.Fatalf("projection validation: %v (%+v)", err, projection)
		}
	}
	preview, err := (localCatalogBulkPreviewer{now: func() time.Time { return now }}).PreviewCatalogBulk(nil, identity, input)
	if err != nil {
		t.Fatal(err)
	}
	if preview.EligibleRows != 1 || preview.Rows[0].After.Draft.Content.Title != "Обновлённый товар" {
		t.Fatalf("preview=%+v", preview)
	}
	if _, err := (localCatalogBulkPreviewer{}).PreviewCatalogBulk(nil, identity, CatalogBulkPreviewInput{Selection: input.Selection, Projections: []catalogbulk.Projection{{SKU: "SKU-1", ProductID: "product-1", ChannelID: "demo", AccountID: "account-1", Draft: func() marketplacelisting.ListingDraft {
		copy := draft
		copy.OrganizationID = "018f0000-0000-7000-8000-000000000009"
		return copy
	}(), Currency: "RUB", PriceMinorUnits: 1, Stock: 1, Version: 1}}, Changes: input.Changes}); err != ErrForbidden {
		t.Fatalf("cross-tenant preview error=%v", err)
	}
}
