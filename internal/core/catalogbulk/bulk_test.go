package catalogbulk

import (
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/marketplacelisting"
)

func bulkFixture(t *testing.T, channel string, state CapabilityState) (ChannelTarget, Projection, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	taxonomy := marketplacelisting.DemoTaxonomy(channel, "ru-RU", "RU", now)
	fingerprint, err := taxonomy.ComputeFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	draft := marketplacelisting.ListingDraft{
		ID: "listing-sku-1", OrganizationID: "org-1", WorkspaceID: "workspace-1", ProductID: "product-1", OfferID: "offer-1", SKU: "SKU-1", CategoryCode: "demo.product", TaxonomyFingerprint: fingerprint, ProductVersion: 1,
		Attributes: map[string]marketplacelisting.AttributeValue{"color": {Value: "white"}},
		Content: marketplacelisting.Content{Locale: "ru-RU", Title: "Товар SKU-1"},
		Media: []marketplacelisting.MediaRef{{ID: "media-1", Slot: "main", ReleasedObjectRef: "upl_media-1", Digest: strings.Repeat("a", 64), Format: "image/jpeg", Bytes: 1024, Width: 1000, Height: 1000, Released: true, Safe: true}},
	}
	target := ChannelTarget{ChannelID: channel, AccountID: "account-1", Label: channel + " demo", State: state, Capabilities: []string{"products.write", "prices.write", "inventory.write", "products.media.write", "products.variants.write"}, TaxonomyFingerprint: fingerprint, TaxonomyVersion: 1, MappingVersion: 1, ObservedAt: now, FreshUntil: now.Add(24 * time.Hour)}
	projection := Projection{SKU: "SKU-1", ProductID: "product-1", OfferID: "offer-1", ChannelID: channel, AccountID: "account-1", Draft: draft, Currency: "RUB", PriceMinorUnits: 10000, Stock: 5, Version: 1}
	return target, projection, now
}

func TestBuildPreviewIsDeterministicAcrossChannelsAndBlocksUnsupportedWrites(t *testing.T) {
	qualified, first, now := bulkFixture(t, "demo", CapabilityQualified)
	readOnly, second, _ := bulkFixture(t, "demo-two", CapabilityReadOnly)
	selection := SelectionSnapshot{FilterDigest: strings.Repeat("b", 64), Filter: "saved_filter", SKUs: []string{"SKU-1"}, Targets: []ChannelTarget{qualified, readOnly}, SnapshotVersion: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	price := int64(12000)
	preview, err := BuildPreview("cbp-1", "org-1", "workspace-1", selection, []Projection{second, first}, []Change{{Kind: "set", Field: "content.title", Value: "Новое название"}, {Kind: "set_price", Field: "price", Currency: "RUB", PriceMinor: &price}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if preview.AffectedSKU != 1 || preview.AffectedRows != 2 || preview.EligibleRows != 1 || preview.BlockedRows != 1 {
		t.Fatalf("preview counts = %+v", preview)
	}
	if preview.Rows[0].ChannelID != "demo" || preview.Rows[0].State != RowReady || preview.Rows[1].State != RowBlocked {
		t.Fatalf("rows = %+v", preview.Rows)
	}
	if !hasCode(preview.Rows[1].Diagnostics, "channel_not_qualified") {
		t.Fatalf("read-only diagnostics = %+v", preview.Rows[1].Diagnostics)
	}
	previewAgain, err := BuildPreview("cbp-2", "org-1", "workspace-1", selection, []Projection{first, second}, []Change{{Kind: "set", Field: "content.title", Value: "Новое название"}, {Kind: "set_price", Field: "price", Currency: "RUB", PriceMinor: &price}}, now)
	if err != nil || preview.InputDigest != previewAgain.InputDigest {
		t.Fatalf("input digest is not stable: first=%v second=%v err=%v", preview.InputDigest, previewAgain.InputDigest, err)
	}
}

func TestApplyChangesKeepsPIMReferencesAndSupportsMediaVariantPriceAndStock(t *testing.T) {
	target, before, now := bulkFixture(t, "demo", CapabilityQualified)
	_ = target
	price := int64(12500)
	stock := int64(9)
	after, err := ApplyChanges(before, []Change{
		{Kind: "set", Field: "content.description", Value: "Подробное описание"},
		{Kind: "set", Field: "attributes.color", Value: "black"},
		{Kind: "set", Field: "attributes.material", Value: "хлопок"},
		{Kind: "replace_media", Field: "media.main", Media: &MediaEdit{AssetID: "media-2", AssetDigest: strings.Repeat("c", 64), Slot: "main", Position: 0, Released: true, Safe: true}},
		{Kind: "set", Field: "variants.item", Value: `{"id":"variant-1","sku":"SKU-1-BLACK","axes":{"color":"black"}}`},
		{Kind: "set_price", Field: "price", Currency: "RUB", PriceMinor: &price},
		{Kind: "set_stock", Field: "stock", Stock: &stock},
	},)
	if err != nil {
		t.Fatal(err)
	}
	if after.Draft.ProductID != before.Draft.ProductID || after.Draft.OrganizationID != before.Draft.OrganizationID || after.PriceMinorUnits != price || after.Stock != stock || after.Draft.Media[0].ID != "media-2" || len(after.Draft.Variants) != 1 {
		t.Fatalf("after = %+v", after)
	}
	if after.Draft.Media[0].ReleasedObjectRef == before.Draft.Media[0].ReleasedObjectRef || !after.Draft.Media[0].Released || !after.Draft.Media[0].Safe {
		t.Fatalf("media replacement = %+v", after.Draft.Media)
	}
	if _, err := ApplyChanges(before, []Change{{Kind: "set", Field: "content.title", Value: "bad\nvalue"}}); err == nil {
		t.Fatal("newline content must be rejected")
	}
	_ = now
}

func TestNewRunKeepsBlockedRowsVisibleAndReconcileUnknown(t *testing.T) {
	qualified, first, now := bulkFixture(t, "demo", CapabilityQualified)
	selection := SelectionSnapshot{FilterDigest: strings.Repeat("d", 64), Filter: "sku", SKUs: []string{"SKU-1"}, Targets: []ChannelTarget{qualified}, SnapshotVersion: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	preview, err := BuildPreview("cbp-3", "org-1", "workspace-1", selection, []Projection{first}, []Change{{Kind: "set", Field: "content.title", Value: "Новое название"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	run, err := NewRun("cbr-1", "idem-12345678", "approval-1", preview, now)
	if err != nil || run.State != StateQueued || len(run.Partitions) != 1 || run.Results[0].State != RowQueued {
		t.Fatalf("run = %+v err=%v", run, err)
	}
	observation := RemoteObservation{RowID: preview.Rows[0].ID, RemoteID: "remote-1", ObservedDigest: strings.Repeat("e", 64), Status: "unknown", ObservedAt: now}
	reconciliation, err := Reconcile(preview.Rows[0], observation)
	if err != nil || reconciliation.Decision != "needs_attention" || len(reconciliation.Drifts) != 2 {
		t.Fatalf("reconciliation = %+v err=%v", reconciliation, err)
	}
}

func hasCode(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
