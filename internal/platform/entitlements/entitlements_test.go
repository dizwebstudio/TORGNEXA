package entitlements

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	orgID    = "01J00000000000000000000001"
	wsID     = "01J00000000000000000000002"
	ruleID   = "01J00000000000000000000003"
	policyID = "01J00000000000000000000004"
	usageID  = "01J00000000000000000000005"
)

type fakeResolver struct {
	rule Rule
	err  error
}

func (f fakeResolver) ResolveRule(context.Context, Scope, FeatureKey, time.Time) (Rule, error) {
	return f.rule, f.err
}

type fakeQuota struct {
	policy QuotaPolicy
	used   int64
}

func (f *fakeQuota) ResolveQuotaPolicy(context.Context, Scope, MetricKey, time.Time) (QuotaPolicy, error) {
	return f.policy, nil
}
func (f *fakeQuota) ConsumeQuota(_ context.Context, _ Scope, p QuotaPolicy, c Consumption) (QuotaStatus, error) {
	if f.used+c.Amount > p.Limit {
		return QuotaStatus{}, ErrQuotaExceeded
	}
	f.used += c.Amount
	start, end, _ := p.Window.Bucket(c.OccurredAt)
	return QuotaStatus{Metric: p.Metric, Limit: p.Limit, Used: f.used, Remaining: p.Limit - f.used, WindowStart: start, WindowEnd: end, PolicyID: p.ID, PolicyVersion: p.Version}, nil
}
func (f *fakeQuota) QuotaStatus(_ context.Context, _ Scope, p QuotaPolicy, at time.Time) (QuotaStatus, error) {
	start, end, _ := p.Window.Bucket(at)
	return QuotaStatus{Metric: p.Metric, Limit: p.Limit, Used: f.used, Remaining: p.Limit - f.used, WindowStart: start, WindowEnd: end, PolicyID: p.ID, PolicyVersion: p.Version}, nil
}

func TestEvaluateFailClosedAndExplicitRule(t *testing.T) {
	at := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	scope, _ := ParseScope(orgID, wsID)
	feature, _ := ParseFeatureKey("connectors.marketplace.write")
	svc, _ := NewService(fakeResolver{err: ErrNotFound})
	got, err := svc.Evaluate(context.Background(), scope, feature, at)
	if err != nil || got.Allowed || got.ReasonCode != "no_entitlement" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	rule := Rule{ID: ruleID, OrganizationID: orgID, WorkspaceID: wsID, Feature: feature, Enabled: true, Source: "operator", Version: 1, EffectiveFrom: at.Add(-time.Hour), CreatedAt: at.Add(-time.Hour)}
	svc, _ = NewService(fakeResolver{rule: rule})
	got, err = svc.Evaluate(context.Background(), scope, feature, at)
	if err != nil || !got.Allowed || got.RuleVersion != 1 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestQuotaExactUTCWindowsAndEnforcement(t *testing.T) {
	at := time.Date(2026, 8, 10, 23, 30, 0, 0, time.UTC)
	scope, _ := ParseScope(orgID, wsID)
	metric, _ := ParseMetricKey("api.requests")
	p := QuotaPolicy{ID: policyID, OrganizationID: orgID, WorkspaceID: wsID, Metric: metric, Limit: 10, Window: WindowDayUTC, Source: "operator", Version: 1, EffectiveFrom: at.Add(-time.Hour), CreatedAt: at.Add(-time.Hour)}
	store := &fakeQuota{policy: p}
	svc, _ := NewQuotaService(store)
	status, err := svc.Consume(context.Background(), scope, Consumption{ID: usageID, Metric: metric, Amount: 7, CorrelationID: "request-1", OccurredAt: at})
	if err != nil || status.Remaining != 3 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	_, err = svc.Consume(context.Background(), scope, Consumption{ID: "01J00000000000000000000006", Metric: metric, Amount: 4, CorrelationID: "request-2", OccurredAt: at})
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("err=%v", err)
	}
	if status.WindowStart.Hour() != 0 || status.WindowEnd.Sub(status.WindowStart) != 24*time.Hour {
		t.Fatalf("window=%s..%s", status.WindowStart, status.WindowEnd)
	}
}

func TestQuotaWindowMonthAndLifetime(t *testing.T) {
	at := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	s, e, err := WindowMonthUTC.Bucket(at)
	if err != nil || s.Day() != 1 || e.Month() != time.March || e.Day() != 1 {
		t.Fatalf("%s %s %v", s, e, err)
	}
	s, e, err = WindowLifetime.Bucket(at)
	if err != nil || s.Year() != 2000 || e.Year() != 9999 {
		t.Fatalf("%s %s %v", s, e, err)
	}
}

func TestRuleAndQuotaRejectNonUTC(t *testing.T) {
	loc := time.FixedZone("x", 3600)
	local := time.Date(2026, 8, 10, 10, 0, 0, 0, loc)
	feature, _ := ParseFeatureKey("reports.export")
	metric, _ := ParseMetricKey("reports.exports")
	if (Rule{ID: ruleID, OrganizationID: orgID, WorkspaceID: wsID, Feature: feature, Enabled: true, Source: "operator", Version: 1, EffectiveFrom: local, CreatedAt: local}).Validate() == nil {
		t.Fatal("non-UTC rule accepted")
	}
	if (QuotaPolicy{ID: policyID, OrganizationID: orgID, WorkspaceID: wsID, Metric: metric, Limit: 1, Window: WindowDayUTC, Source: "operator", Version: 1, EffectiveFrom: local, CreatedAt: local}).Validate() == nil {
		t.Fatal("non-UTC quota accepted")
	}
}
