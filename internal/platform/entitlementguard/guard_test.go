package entitlementguard

import (
	"context"
	"github.com/torgnexa/torgnexa/internal/platform/entitlements"
	"testing"
	"time"
)

type fakes struct {
	allowed                  bool
	featureCalls, quotaCalls int
}

func (f *fakes) Evaluate(_ context.Context, _ entitlements.Scope, k entitlements.FeatureKey, at time.Time) (entitlements.Evaluation, error) {
	f.featureCalls++
	return entitlements.Evaluation{Feature: k, Allowed: f.allowed, ReasonCode: map[bool]string{true: "entitlement_enabled", false: "entitlement_disabled"}[f.allowed], EvaluatedAt: at}, nil
}
func (f *fakes) Consume(_ context.Context, _ entitlements.Scope, c entitlements.Consumption) (entitlements.QuotaStatus, error) {
	f.quotaCalls++
	return entitlements.QuotaStatus{Metric: c.Metric, Limit: 10, Used: c.Amount, Remaining: 10 - c.Amount, WindowStart: c.OccurredAt.Truncate(24 * time.Hour), WindowEnd: c.OccurredAt.Truncate(24 * time.Hour).Add(24 * time.Hour), PolicyID: "01J00000000000000000000009", PolicyVersion: 1}, nil
}
func TestDeniedFeatureDoesNotConsumeQuota(t *testing.T) {
	at := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	s, _ := entitlements.ParseScope("01J00000000000000000000001", "01J00000000000000000000002")
	ft, _ := entitlements.ParseFeatureKey("reports.export")
	m, _ := entitlements.ParseMetricKey("reports.exports")
	f := &fakes{}
	g, _ := New(f, f)
	_, err := g.Authorize(context.Background(), s, Requirement{Feature: ft, Metric: m, Amount: 1, UsageID: "01J00000000000000000000003", CorrelationID: "req-1", At: at})
	if err == nil || f.quotaCalls != 0 {
		t.Fatalf("err=%v quota=%d", err, f.quotaCalls)
	}
}
func TestAllowedFeatureConsumesQuotaOnce(t *testing.T) {
	at := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	s, _ := entitlements.ParseScope("01J00000000000000000000001", "01J00000000000000000000002")
	ft, _ := entitlements.ParseFeatureKey("reports.export")
	m, _ := entitlements.ParseMetricKey("reports.exports")
	f := &fakes{allowed: true}
	g, _ := New(f, f)
	d, err := g.Authorize(context.Background(), s, Requirement{Feature: ft, Metric: m, Amount: 1, UsageID: "01J00000000000000000000003", CorrelationID: "req-1", At: at})
	if err != nil || f.quotaCalls != 1 || d.Quota == nil {
		t.Fatalf("d=%+v err=%v calls=%d", d, err, f.quotaCalls)
	}
}
