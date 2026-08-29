package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
)

type realtimeAuditStub struct{ rows []audit.Record }

func (s realtimeAuditStub) List(context.Context, tenancy.Scope, int, string) ([]audit.Record, string, error) {
	return s.rows, "", nil
}

type realtimeLatestAuditStub struct {
	latest    string
	listCalls int
}

func (s *realtimeLatestAuditStub) List(context.Context, tenancy.Scope, int, string) ([]audit.Record, string, error) {
	s.listCalls++
	return []audit.Record{{ID: s.latest}}, "", nil
}

func (s *realtimeLatestAuditStub) LatestID(context.Context, tenancy.Scope) (string, error) {
	return s.latest, nil
}

type cancellingRecorder struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (r *cancellingRecorder) Flush() { r.cancel() }

func TestRealtimeStreamIsMetadataOnlyAndTenantScoped(t *testing.T) {
	scope, err := tenancy.ParseScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	stub := realtimeAuditStub{rows: []audit.Record{{ID: "018f0000-0000-7000-8000-000000000003", OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), ActorID: "user", Source: "api", Action: "orders.updated", ResourceType: "order", ResourceID: "secret-order-id", Risk: audit.RiskRead, Summary: audit.Summary{"pii": "must-not-stream"}, CreatedAt: now}}}
	route := newRealtimeRoutes(stub)[0]
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), requestScopeKey{}, scope))
	req := httptest.NewRequest(http.MethodGet, RealtimePath, nil).WithContext(ctx)
	rec := &cancellingRecorder{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	route.Handler.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "event: ready") || !strings.Contains(body, "connected") {
		t.Fatalf("missing ready event: %s", body)
	}
	if strings.Contains(body, "secret-order-id") || strings.Contains(body, "must-not-stream") {
		t.Fatalf("raw audit payload leaked into stream: %s", body)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content-type=%q", got)
	}
}

func TestRealtimeStreamUsesLatestIDFastPath(t *testing.T) {
	scope, err := tenancy.ParseScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	stub := &realtimeLatestAuditStub{latest: "018f0000-0000-7000-8000-000000000003"}
	route := newRealtimeRoutes(stub)[0]
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), requestScopeKey{}, scope))
	defer cancel()
	rec := &cancellingRecorder{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	route.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, RealtimePath, nil).WithContext(ctx))
	if stub.listCalls != 0 {
		t.Fatalf("realtime fast path fell back to full audit list %d times", stub.listCalls)
	}
}
