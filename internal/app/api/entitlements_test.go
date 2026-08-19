package api

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/platform/entitlements"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type entScope struct{ bad bool }

func (entScope) EntitlementScope(*http.Request) (entitlements.Scope, error) {
	s, _ := entitlements.ParseScope("01J00000000000000000000001", "01J00000000000000000000002")
	return s, nil
}

type entSvc struct{}

func (entSvc) Evaluate(_ context.Context, _ entitlements.Scope, f entitlements.FeatureKey, at time.Time) (entitlements.Evaluation, error) {
	return entitlements.Evaluation{Feature: f, Allowed: true, ReasonCode: "entitlement_enabled", RuleID: "01J00000000000000000000003", RuleVersion: 1, EvaluatedAt: at}, nil
}

type quotaSvc struct{}

func (quotaSvc) Status(_ context.Context, _ entitlements.Scope, m entitlements.MetricKey, at time.Time) (entitlements.QuotaStatus, error) {
	start, _, _ := entitlements.WindowDayUTC.Bucket(at)
	return entitlements.QuotaStatus{Metric: m, Limit: 100, Used: 7, Remaining: 93, WindowStart: start, WindowEnd: start.Add(24 * time.Hour), PolicyID: "01J00000000000000000000004", PolicyVersion: 1}, nil
}

type fixedClock struct{ at time.Time }

func (f fixedClock) Now() time.Time { return f.at }
func TestEntitlementEvaluate(t *testing.T) {
	at := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	h := newHandlerWithEntitlementsClock(slog.Default(), entSvc{}, quotaSvc{}, entScope{}, fixedClock{at})
	r := httptest.NewRequest(http.MethodPost, EntitlementEvaluatePath, strings.NewReader(`{"feature":"reports.export","metric":"reports.exports"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"remaining":93`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
}
func TestEntitlementEvaluateRejectsUnknown(t *testing.T) {
	h := newHandlerWithEntitlementsClock(slog.Default(), entSvc{}, quotaSvc{}, entScope{}, fixedClock{time.Now().UTC()})
	r := httptest.NewRequest(http.MethodPost, EntitlementEvaluatePath, strings.NewReader(`{"feature":"x","unknown":1}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

var _ = errors.New
