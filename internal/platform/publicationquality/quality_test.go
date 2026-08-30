package publicationquality

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

func qualityMoney(t *testing.T, minor int64, currency string) domain.Money {
	t.Helper()
	value, err := domain.NewCurrency(currency)
	if err != nil {
		t.Fatal(err)
	}
	money, err := domain.NewMoney(minor, value)
	if err != nil {
		t.Fatal(err)
	}
	return money
}

func qualityQuantity(t *testing.T, coefficient int64, unit string) domain.Quantity {
	t.Helper()
	value, err := domain.NewDecimal(coefficient, 0)
	if err != nil {
		t.Fatal(err)
	}
	code, err := domain.NewUnitCode(unit)
	if err != nil {
		t.Fatal(err)
	}
	quantity, err := domain.NewQuantity(value, code)
	if err != nil {
		t.Fatal(err)
	}
	return quantity
}

func qualityProfile(t *testing.T) Profile {
	t.Helper()
	return Profile{
		ID: "storefront-demo-v1", Version: 1, ConnectorID: "storefront-demo", ChannelFamily: "storefront", Locale: "ru-RU", Jurisdiction: "RU",
		FreshnessTTL: 24 * time.Hour, Active: true, RequiredMedia: 1, AllowedMediaFormats: []string{"jpg", "png"}, MaxMediaBytes: 10_000_000, RequirePrice: true, RequireStock: true, Currency: "RUB",
		Weights: map[Category]int64{CategoryIdentity: 2, CategoryAttributes: 1, CategoryMedia: 1, CategoryPriceStock: 2, CategoryCompliance: 2, CategoryCapability: 2, CategoryLocalization: 1, CategoryContract: 1},
		Rules:   []Rule{{ID: "brand_required", Category: CategoryAttributes, Field: "brand", Kind: RuleRequired, Severity: SeverityBlock, Message: "brand is required", Remediation: "set brand"}},
	}
}

func qualitySnapshot(t *testing.T, now time.Time) Snapshot {
	t.Helper()
	target := Target{OrganizationID: "org-1", WorkspaceID: "ws-1", ProductID: "product-1", OfferID: "offer-1", ConnectorAccountID: "account-1", ConnectorID: "storefront-demo", ChannelFamily: "storefront", Locale: "ru-RU", Jurisdiction: "RU"}
	fingerprint := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	return Snapshot{
		Target: target, ProductVersion: 2, OfferVersion: 1, PriceVersion: 3, InventoryVersion: 4, MediaVersion: 5, MappingVersion: 6, CapabilityVersion: 7,
		SKU: "SKU-1", GTIN: "", Title: "Демо товар", Description: "Описание", CategoryCode: "electronics", Attributes: map[string]string{"brand": "TORGNEXA"},
		Media: []MediaAsset{{ID: "asset-1", Format: "jpg", Bytes: 1000, Width: 800, Height: 800, Released: true, Safe: true}},
		Price: func() *domain.Money { value := qualityMoney(t, 19900, "RUB"); return &value }(), Available: func() *domain.Quantity { value := qualityQuantity(t, 10, "PCS"); return &value }(),
		Compliance: ComplianceEvidence{Outcome: "allow", Fingerprint: fingerprint, EvaluatedAt: now.Add(-time.Hour)}, CompliancePresent: true,
		MappingConfigured: true, CapabilityAdmitted: true, ProductsWriteEnabled: true, SourceFreshAt: now.Add(-time.Hour), AssembledAt: now,
		CatalogDigest: fingerprint, PIMDigest: fingerprint, MediaDigest: fingerprint,
	}
}

func TestEvaluateProducesStableReadyReceipt(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	profile := qualityProfile(t)
	snapshot := qualitySnapshot(t, now)
	run, receipt, err := Evaluate(profile, snapshot, now)
	if err != nil {
		t.Fatal(err)
	}
	if run.Decision != DecisionReady || run.ScoreBPS != 10_000 {
		t.Fatalf("unexpected ready result: decision=%s score=%d issues=%v", run.Decision, run.ScoreBPS, run.Issues)
	}
	if run.ComplianceFingerprint != snapshot.Compliance.Fingerprint {
		t.Fatalf("run lost compliance lineage: got %q want %q", run.ComplianceFingerprint, snapshot.Compliance.Fingerprint)
	}
	if err := CheckReceipt(receipt, snapshot, profile, now.Add(time.Minute)); err != nil {
		t.Fatalf("receipt should authorize exact snapshot: %v", err)
	}
	changed := snapshot
	changed.ProductVersion++
	if !errors.Is(CheckReceipt(receipt, changed, profile, now.Add(time.Minute)), ErrReceiptStale) {
		t.Fatal("changed product version reused a receipt")
	}
	secondRun, secondReceipt, err := Evaluate(profile, snapshot, now)
	if err != nil || secondRun.ID != run.ID || secondReceipt.ID != receipt.ID || secondRun.SnapshotDigest != run.SnapshotDigest || secondRun.ProfileDigest != run.ProfileDigest {
		t.Fatalf("evaluation is not deterministic: %#v %#v %v", secondRun, secondReceipt, err)
	}
}

func TestDeclarativeDecimalRulesAndProfileDigest(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	profile := qualityProfile(t)
	minimum, err := domain.NewDecimal(20_000, 0)
	if err != nil {
		t.Fatal(err)
	}
	maximum, err := domain.NewDecimal(50_000, 0)
	if err != nil {
		t.Fatal(err)
	}
	profile.Rules = append(profile.Rules,
		Rule{ID: "price_min", Category: CategoryPriceStock, Field: "price_minor", Kind: RuleMinValue, Min: &minimum, Severity: SeverityBlock, Message: "minimum price", Remediation: "raise price"},
		Rule{ID: "price_max", Category: CategoryPriceStock, Field: "price_minor", Kind: RuleMaxValue, Max: &maximum, Severity: SeverityWarn, Message: "maximum price", Remediation: "review price"},
	)
	if _, err := profile.Digest(); err != nil {
		t.Fatal(err)
	}
	withoutBounds := profile
	withoutBounds.Rules = withoutBounds.Rules[:1]
	first, err := profile.Digest()
	if err != nil {
		t.Fatal(err)
	}
	second, err := withoutBounds.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("profile digest ignored declarative rules")
	}
	snapshot := qualitySnapshot(t, now)
	run, _, err := Evaluate(profile, snapshot, now)
	if err != nil {
		t.Fatal(err)
	}
	if run.Decision != DecisionBlocked {
		t.Fatalf("minimum decimal rule did not block: %s (%v)", run.Decision, run.Issues)
	}
}

func TestUnsafeDecimalRuleRequiresBound(t *testing.T) {
	profile := qualityProfile(t)
	profile.Rules = append(profile.Rules, Rule{ID: "missing_min", Category: CategoryPriceStock, Field: "amount", Kind: RuleMinValue, Severity: SeverityBlock, Message: "minimum", Remediation: "fix"})
	if _, err := profile.Digest(); !errors.Is(err, ErrProfileUnsafe) {
		t.Fatalf("missing min bound accepted: %v", err)
	}
}

func TestQualityRunLifecycleAllowsQueuedAndRequiresTerminalTimes(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	snapshot := qualitySnapshot(t, now)
	run, _, err := Evaluate(qualityProfile(t), snapshot, now)
	if err != nil {
		t.Fatal(err)
	}
	queued := run
	queued.Status = "queued"
	queued.Decision = DecisionUnknown
	queued.EvaluatedAt = time.Time{}
	queued.ValidUntil = time.Time{}
	if err := queued.Validate(); err != nil {
		t.Fatalf("queued run should be valid before evaluation: %v", err)
	}
	terminal := queued
	terminal.Status = "completed"
	if err := terminal.Validate(); err == nil {
		t.Fatal("completed run without evaluation timestamps was accepted")
	}
}

func TestEvaluateBlocksBeforeRemoteWriteAndOrdersIssues(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	profile := qualityProfile(t)
	snapshot := qualitySnapshot(t, now)
	snapshot.Attributes = map[string]string{}
	snapshot.Media[0].Released = false
	snapshot.MappingConfigured = false
	snapshot.CompliancePresent = false
	run, receipt, err := Evaluate(profile, snapshot, now)
	if err != nil {
		t.Fatal(err)
	}
	if run.Decision != DecisionBlocked || receipt.ID != "" {
		t.Fatalf("blocked snapshot produced a gate receipt: decision=%s receipt=%+v", run.Decision, receipt)
	}
	if len(run.Issues) < 4 || run.Issues[0].Severity != SeverityBlock {
		t.Fatalf("expected deterministic hard issues first: %+v", run.Issues)
	}
	if !errors.Is(CheckReceipt(receipt, snapshot, profile, now), ErrGateDenied) {
		t.Fatal("empty receipt did not deny publication")
	}
}

func TestEvaluateDistinguishesStaleUnsupportedAndUnknown(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	profile := qualityProfile(t)
	snapshot := qualitySnapshot(t, now)
	snapshot.SourceFreshAt = now.Add(-48 * time.Hour)
	run, _, err := Evaluate(profile, snapshot, now)
	if err != nil || run.Decision != DecisionStale {
		t.Fatalf("expected stale decision, got %s (%v)", run.Decision, err)
	}
	snapshot = qualitySnapshot(t, now)
	snapshot.CapabilityAdmitted = false
	run, _, err = Evaluate(profile, snapshot, now)
	if err != nil || run.Decision != DecisionUnsupported {
		t.Fatalf("expected unsupported decision, got %s (%v)", run.Decision, err)
	}
	snapshot = qualitySnapshot(t, now)
	snapshot.CompliancePresent = false
	run, _, err = Evaluate(profile, snapshot, now)
	if err != nil || run.Decision != DecisionUnknown {
		t.Fatalf("expected unknown decision, got %s (%v)", run.Decision, err)
	}
	snapshot = qualitySnapshot(t, now)
	snapshot.ProductStatus = "archived"
	run, _, err = Evaluate(profile, snapshot, now)
	if err != nil || run.Decision != DecisionBlocked {
		t.Fatalf("expected archived product to be blocked, got %s (%v)", run.Decision, err)
	}
}

func TestMemoryStoreIsTenantScopedAndConflictSafe(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	run, receipt, err := Evaluate(qualityProfile(t), qualitySnapshot(t, now), now)
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	if err := store.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Run("other-org", "ws-1", run.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant run lookup returned %v", err)
	}
	conflict := run
	conflict.SnapshotDigest = strings.Repeat("f", 64)
	if err := store.SaveRun(conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected run conflict, got %v", err)
	}
}
