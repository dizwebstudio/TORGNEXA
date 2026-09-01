package marketplacelisting

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func testTaxonomy(t *testing.T) (Taxonomy, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	taxonomy := DemoTaxonomy("demo", "ru-RU", "RU", now)
	if err := taxonomy.Validate(); err != nil {
		t.Fatalf("DemoTaxonomy().Validate() error = %v", err)
	}
	return taxonomy, now
}

func testDraft(t *testing.T, taxonomy Taxonomy, now time.Time, sku string, color string) ListingDraft {
	t.Helper()
	_ = now
	fingerprint, err := taxonomy.ComputeFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return ListingDraft{
		ID:                  "listing-" + sku,
		OrganizationID:      "org-1",
		WorkspaceID:         "workspace-1",
		ProductID:           "product-" + sku,
		OfferID:             "offer-" + sku,
		SKU:                 sku,
		CategoryCode:        "demo.product",
		TaxonomyFingerprint: fingerprint,
		ProductVersion:      1,
		Attributes: map[string]AttributeValue{
			"color": {Value: color},
		},
		Content: Content{Locale: "ru-RU", Title: "Товар " + sku},
		Media: []MediaRef{{
			ID: "media-" + sku, Slot: "main", ReleasedObjectRef: "upl_" + sku,
			Digest: strings.Repeat("a", 64), Format: "image/jpeg", Bytes: 1024,
			Width: 1000, Height: 1000, Released: true, Safe: true,
		}},
	}
}

func TestValidateDraftHonoursConditionalAttributesAndReleaseGate(t *testing.T) {
	taxonomy, now := testTaxonomy(t)
	black := testDraft(t, taxonomy, now, "sku-1", "black")
	issues := ValidateDraft(taxonomy, black, now)
	if len(issues) != 1 || issues[0].Code != "missing_attribute" || issues[0].FieldPath != "attributes.material" {
		t.Fatalf("black draft diagnostics = %#v, want missing material", issues)
	}
	black.Attributes["material"] = AttributeValue{Value: "хлопок"}
	if issues = ValidateDraft(taxonomy, black, now); len(issues) != 0 {
		t.Fatalf("valid black draft diagnostics = %#v", issues)
	}
	white := testDraft(t, taxonomy, now, "sku-2", "white")
	if issues = ValidateDraft(taxonomy, white, now); len(issues) != 0 {
		t.Fatalf("valid white draft diagnostics = %#v", issues)
	}
	black.Media[0].Released = false
	issues = ValidateDraft(taxonomy, black, now)
	if !hasDiagnostic(issues, "media_not_released") {
		t.Fatalf("diagnostics = %#v, want media_not_released", issues)
	}
	if issues = ValidateDraft(taxonomy, white, now.Add(25*time.Hour)); !hasDiagnostic(issues, "stale_taxonomy") {
		t.Fatalf("stale diagnostics = %#v, want stale_taxonomy", issues)
	}
}

func TestMappingRequiresCurrentTaxonomyAndIsDeterministic(t *testing.T) {
	taxonomy, now := testTaxonomy(t)
	fingerprint, _ := taxonomy.ComputeFingerprint()
	mapping := Mapping{
		ID: "mapping-1", Version: 1, TaxonomyFingerprint: fingerprint,
		Entries: []MappingEntry{{SourceField: "source.color", TargetCode: "color", Transform: "lower", EnumMap: map[string]string{"BLACK": "black"}}},
	}
	values, err := ApplyMapping(mapping, taxonomy, map[string]AttributeValue{"source.color": {Value: "BLACK"}})
	if err != nil || values["color"].Value != "black" {
		t.Fatalf("ApplyMapping() values = %#v, error = %v", values, err)
	}
	old := taxonomy
	old.Version++
	if _, err = ApplyMapping(mapping, old, map[string]AttributeValue{"source.color": {Value: "BLACK"}}); !errors.Is(err, ErrStaleTaxonomy) {
		t.Fatalf("ApplyMapping(stale) error = %v, want %v", err, ErrStaleTaxonomy)
	}
	_ = now
}

func TestMappingConvertsSupportedUnitsExactly(t *testing.T) {
	taxonomy, now := testTaxonomy(t)
	taxonomy.Attributes = append(taxonomy.Attributes, AttributeDefinition{Code: "length", Name: "Длина", ValueType: ValueDimension, Requirement: RequirementOptional, Unit: "mm"})
	taxonomy.Categories[0].AttributeCodes = append(taxonomy.Categories[0].AttributeCodes, "length")
	fingerprint, err := taxonomy.ComputeFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	mapping := Mapping{ID: "mapping-units", Version: 1, TaxonomyFingerprint: fingerprint, Entries: []MappingEntry{{SourceField: "source.length", TargetCode: "length", UnitFrom: "cm", UnitTo: "mm"}}}
	values, err := ApplyMapping(mapping, taxonomy, map[string]AttributeValue{"source.length": {Value: "1.25", Unit: "cm"}})
	if err != nil || values["length"].Value != "12.5" || values["length"].Unit != "mm" {
		t.Fatalf("ApplyMapping(unit) values = %#v, error = %v", values, err)
	}
	_ = now
}

func TestBatchOperationsRejectUnknownFieldsAndDuplicateVariants(t *testing.T) {
	if err := (BatchOperation{Kind: BatchSet, Field: "remote.unsupported", Value: "value"}).Validate(); err == nil {
		t.Fatal("unknown batch field was accepted")
	}
	taxonomy, now := testTaxonomy(t)
	draft := testDraft(t, taxonomy, now, "sku-1", "white")
	draft.Variants = []Variant{{ID: "variant-1", SKU: "sku-2", Axes: map[string]string{"color": "black"}}, {ID: "variant-2", SKU: "sku-3", Axes: map[string]string{"color": "black"}}}
	if err := draft.Validate(); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate variant combination error = %v, want %v", err, ErrConflict)
	}
}

func TestBuildBatchPreviewBoundsSortsAndBlocksRows(t *testing.T) {
	taxonomy, now := testTaxonomy(t)
	valid := testDraft(t, taxonomy, now, "sku-b", "white")
	blocked := testDraft(t, taxonomy, now, "sku-a", "black")
	preview, err := BuildBatchPreview("preview-1", "org-1", "workspace-1", "account-1", "demo", taxonomy, []BatchItem{{SKU: valid.SKU, Before: valid}, {SKU: blocked.SKU, Before: blocked}}, []BatchOperation{{Kind: BatchSet, Field: "content.title", Value: "Обновлённый товар"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Rows[0].SKU != "sku-a" || preview.AffectedCount != 2 || preview.EligibleCount != 1 || preview.BlockedCount != 1 {
		t.Fatalf("preview summary = %#v", preview)
	}
	if !hasDiagnostic(preview.Rows[0].Diagnostics, "missing_attribute") || !preview.Rows[1].Eligible {
		t.Fatalf("preview rows = %#v", preview.Rows)
	}
	items := make([]BatchItem, 0, MaxBatchItems+1)
	for i := 0; i < MaxBatchItems+1; i++ {
		item := testDraft(t, taxonomy, now, "sku-"+strings.Repeat("x", 1)+string(rune('a'+i%26))+string(rune('0'+i/26)), "white")
		item.ID = "listing-" + item.SKU + "-" + string(rune('a'+i/26))
		items = append(items, BatchItem{SKU: item.SKU, Before: item})
	}
	if _, err = BuildBatchPreview("preview-2", "org-1", "workspace-1", "account-1", "demo", taxonomy, items, nil, now); !errors.Is(err, ErrBatchTooLarge) {
		t.Fatalf("oversized preview error = %v, want %v", err, ErrBatchTooLarge)
	}
}

func TestReconcileReportsUnknownAndRemoteDrift(t *testing.T) {
	taxonomy, now := testTaxonomy(t)
	draft := testDraft(t, taxonomy, now, "sku-1", "white")
	digest, err := DraftDigest(draft)
	if err != nil {
		t.Fatal(err)
	}
	drifts, err := Reconcile(draft, digest, RemoteObservation{RemoteID: "remote-1", SnapshotDigest: strings.Repeat("b", 64), Status: "unknown", CategoryCode: "other.category", ObservedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(drifts) != 3 || !hasDrift(drifts, DriftUnknownOutcome) || !hasDrift(drifts, DriftContentMismatch) || !hasDrift(drifts, DriftCategoryMismatch) {
		t.Fatalf("drifts = %#v", drifts)
	}
}

func hasDiagnostic(items []Diagnostic, code string) bool {
	for _, item := range items {
		if item.Code == code {
			return true
		}
	}
	return false
}

func hasDrift(items []Drift, kind DriftType) bool {
	for _, item := range items {
		if item.Type == kind {
			return true
		}
	}
	return false
}
