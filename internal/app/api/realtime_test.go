package api

import (
	"context"
	"errors"
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

// These tests inspect metadata, not socket deadlines. The HTTP integration
// suite exercises actual write-deadline support and failures.
func (r *cancellingRecorder) SetWriteDeadline(time.Time) error { return nil }

type a10FailingStream struct {
	*httptest.ResponseRecorder
	deadlineFailureAt int
	deadlineCalls     int
	flushCalls        int
	writeFailure      bool
	flushFailure      bool
}

func (w *a10FailingStream) SetWriteDeadline(time.Time) error {
	w.deadlineCalls++
	if w.deadlineCalls == w.deadlineFailureAt {
		return errors.New("synthetic control failure")
	}
	return nil
}

func (w *a10FailingStream) Write(p []byte) (int, error) {
	if w.writeFailure {
		return 0, errors.New("synthetic write failure")
	}
	return w.ResponseRecorder.Write(p)
}

func (w *a10FailingStream) FlushError() error {
	w.flushCalls++
	if w.flushFailure {
		return errors.New("synthetic flush failure")
	}
	w.ResponseRecorder.Flush()
	return nil
}

func TestA10RealtimeWriteFailuresStopStreaming(t *testing.T) {
	for _, tc := range []struct {
		name         string
		deadlineFail int
		writeFail    bool
		flushFail    bool
		wantFlushes  int
	}{
		{"frame_deadline", 2, false, false, 0},
		{"write", 0, true, false, 0},
		{"flush", 0, false, true, 1},
		{"idle_deadline_clear", 3, false, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			head := &a10AuditHead{scope: validTestScope(t)}
			head.id.Store("synthetic-head")
			ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), requestScopeKey{}, head.scope), time.Second)
			defer cancel()
			w := &a10FailingStream{ResponseRecorder: httptest.NewRecorder(), deadlineFailureAt: tc.deadlineFail, writeFailure: tc.writeFail, flushFailure: tc.flushFail}
			newRealtimeRoutes(head)[0].Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, RealtimePath, nil).WithContext(ctx))
			if ctx.Err() != nil || head.calls.Load() != 1 || w.flushCalls != tc.wantFlushes {
				t.Fatal("failed write/control continued streaming or ignored FlushError")
			}
			if strings.Contains(w.Body.String(), "synthetic") && strings.Contains(w.Body.String(), "failure") {
				t.Fatal("internal stream failure leaked into SSE")
			}
		})
	}
}

func TestA10RealtimeRequiresDeadlineSupport(t *testing.T) {
	for _, supported := range []bool{false, true} {
		t.Run(map[bool]string{false: "unsupported", true: "control_error"}[supported], func(t *testing.T) {
			head := &a10AuditHead{scope: validTestScope(t)}
			head.id.Store("synthetic-head")
			ctx := context.WithValue(t.Context(), requestScopeKey{}, head.scope)
			recorder := httptest.NewRecorder()
			var w http.ResponseWriter = recorder
			want := http.StatusNotImplemented
			if supported {
				w = &a10FailingStream{ResponseRecorder: recorder, deadlineFailureAt: 1}
				want = http.StatusServiceUnavailable
			}
			newRealtimeRoutes(head)[0].Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, RealtimePath, nil).WithContext(ctx))
			if recorder.Code != want || head.calls.Load() != 0 || strings.Contains(recorder.Body.String(), "synthetic") || strings.Contains(recorder.Header().Get("Content-Type"), "event-stream") {
				t.Fatal("stream started without working write-deadline support")
			}
		})
	}
}

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
