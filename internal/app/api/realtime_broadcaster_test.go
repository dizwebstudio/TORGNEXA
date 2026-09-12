package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
)

type realtimeMultiTenantHead struct {
	calls atomic.Uint64
	id    string
}

type realtimeFailOnceHead struct {
	calls atomic.Uint64
}

func (h *realtimeFailOnceHead) List(context.Context, tenancy.Scope, int, string) ([]audit.Record, string, error) {
	return nil, "", errors.New("realtime must use the metadata-only lookup")
}

func (h *realtimeFailOnceHead) LatestID(ctx context.Context, _ tenancy.Scope) (string, error) {
	call := h.calls.Add(1)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if call == 1 {
		return "", errors.New("synthetic audit outage")
	}
	return "recovered-head", nil
}

func (h *realtimeMultiTenantHead) List(context.Context, tenancy.Scope, int, string) ([]audit.Record, string, error) {
	return nil, "", errors.New("realtime must use the metadata-only lookup")
}

func (h *realtimeMultiTenantHead) LatestID(ctx context.Context, _ tenancy.Scope) (string, error) {
	h.calls.Add(1)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return h.id, nil
}

func realtimeOtherScope(t *testing.T) tenancy.Scope {
	t.Helper()
	scope, err := tenancy.ParseScope("018f0000-0000-7000-8000-000000000011", "018f0000-0000-7000-8000-000000000012")
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func TestRealtimeBroadcasterEnforcesTenantAndProcessLimits(t *testing.T) {
	head := &realtimeMultiTenantHead{id: "synthetic-head"}
	broadcaster := newRealtimeBroadcaster(head, realtimeTiming{
		pollInterval:         time.Hour,
		maxClientsPerTenant:  2,
		maxClientsPerProcess: 3,
		subscriberBuffer:     1,
	})
	scope := validTestScope(t)
	first, err := broadcaster.subscribe(t.Context(), scope)
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	second, err := broadcaster.subscribe(t.Context(), scope)
	if err != nil {
		t.Fatal(err)
	}
	defer second.close()
	if _, err = broadcaster.subscribe(t.Context(), scope); !errors.Is(err, errRealtimeTenantClientLimit) {
		t.Fatalf("third tenant client error=%v", err)
	}

	other := realtimeOtherScope(t)
	third, err := broadcaster.subscribe(t.Context(), other)
	if err != nil {
		t.Fatal(err)
	}
	defer third.close()
	thirdScope, err := tenancy.ParseScope("018f0000-0000-7000-8000-000000000021", "018f0000-0000-7000-8000-000000000022")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = broadcaster.subscribe(t.Context(), thirdScope); !errors.Is(err, errRealtimeProcessClientLimit) {
		t.Fatalf("fourth process client error=%v", err)
	}

	metrics := broadcaster.RealtimeBroadcasterMetrics()
	if metrics.ActiveTenants != 2 || metrics.ActiveClients != 3 || metrics.PeakClients != 3 || metrics.RejectedTenant != 1 || metrics.RejectedProcess != 1 {
		t.Fatalf("unexpected saturation metrics: %+v", metrics)
	}
	if head.calls.Load() != 2 {
		t.Fatalf("audit queries=%d, want one per active tenant", head.calls.Load())
	}
	broadcaster.mu.Lock()
	var tenantWatcher *realtimeTenantWatcher
	for _, watcher := range broadcaster.tenants {
		if watcher.scope == scope {
			tenantWatcher = watcher
		}
	}
	if tenantWatcher == nil {
		broadcaster.mu.Unlock()
		t.Fatal("tenant A watcher is missing")
	}
	broadcaster.publishLocked(tenantWatcher, realtimeSignal{cursor: "tenant-a-change"})
	broadcaster.mu.Unlock()
	select {
	case signal := <-first.events:
		if signal.cursor != "tenant-a-change" {
			t.Fatalf("tenant A cursor=%q", signal.cursor)
		}
	case <-time.After(time.Second):
		t.Fatal("tenant A did not receive its invalidation")
	}
	select {
	case signal := <-third.events:
		t.Fatalf("tenant B received tenant A cursor %q", signal.cursor)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestRealtimeBroadcasterCoalescesBoundedClientBuffer(t *testing.T) {
	head := &realtimeMultiTenantHead{id: "synthetic-head"}
	broadcaster := newRealtimeBroadcaster(head, realtimeTiming{pollInterval: time.Hour, subscriberBuffer: 1})
	subscription, err := broadcaster.subscribe(t.Context(), validTestScope(t))
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.close()

	broadcaster.mu.Lock()
	var watcher *realtimeTenantWatcher
	for _, candidate := range broadcaster.tenants {
		watcher = candidate
	}
	for _, cursor := range []string{"audit-1", "audit-2", "audit-3"} {
		broadcaster.publishLocked(watcher, realtimeSignal{cursor: cursor})
	}
	broadcaster.mu.Unlock()

	select {
	case signal := <-subscription.events:
		if signal.cursor != "audit-3" {
			t.Fatalf("pending cursor=%q, want newest", signal.cursor)
		}
	case <-time.After(time.Second):
		t.Fatal("missing coalesced invalidation")
	}
	if metrics := broadcaster.RealtimeBroadcasterMetrics(); metrics.DeliveriesMerged != 2 || metrics.DeliveriesQueued != 3 {
		t.Fatalf("unexpected coalescing metrics: %+v", metrics)
	}
}

func TestRealtimeBroadcasterRecoversAfterAuditHeadFailure(t *testing.T) {
	head := &realtimeFailOnceHead{}
	broadcaster := newRealtimeBroadcaster(head, realtimeTiming{pollInterval: 10 * time.Millisecond, pollTimeout: 100 * time.Millisecond})
	subscription, err := broadcaster.subscribe(t.Context(), validTestScope(t))
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.close()
	if subscription.initial != "" {
		t.Fatalf("failed initial query returned cursor %q", subscription.initial)
	}
	select {
	case signal := <-subscription.events:
		if signal.cursor != "recovered-head" {
			t.Fatalf("recovered cursor=%q", signal.cursor)
		}
	case <-time.After(time.Second):
		t.Fatal("tenant watcher did not recover after audit-head failure")
	}
	metrics := broadcaster.RealtimeBroadcasterMetrics()
	if metrics.AuditHeadQueries < 2 || metrics.AuditHeadErrors != 1 || metrics.SignalsPublished != 1 {
		t.Fatalf("unexpected recovery metrics: %+v", metrics)
	}
}

type realtimeLoadRecorder struct {
	*httptest.ResponseRecorder
	ready        chan struct{}
	invalidation chan struct{}
	readyOnce    sync.Once
	auditOnce    sync.Once
	flushes      atomic.Int32
}

func newRealtimeLoadRecorder() *realtimeLoadRecorder {
	return &realtimeLoadRecorder{ResponseRecorder: httptest.NewRecorder(), ready: make(chan struct{}), invalidation: make(chan struct{})}
}

func (w *realtimeLoadRecorder) SetWriteDeadline(time.Time) error { return nil }

func (w *realtimeLoadRecorder) FlushError() error {
	w.ResponseRecorder.Flush()
	flushes := w.flushes.Add(1)
	if flushes >= 2 {
		w.readyOnce.Do(func() { close(w.ready) })
	}
	if flushes >= 3 {
		w.auditOnce.Do(func() { close(w.invalidation) })
	}
	return nil
}

func TestRealtimeReconnectStormSharesOneTenantQueryPerWave(t *testing.T) {
	const (
		clients = 48
		waves   = 6
	)
	head := &a10AuditHead{scope: validTestScope(t)}
	head.id.Store("synthetic-head")
	routes, broadcaster := newRealtimeRoutesWithBroadcaster(head, realtimeTiming{
		pollInterval:      time.Hour,
		heartbeatInterval: time.Hour,
		writeTimeout:      time.Second,
	})
	route := routes[0]

	for wave := 1; wave <= waves; wave++ {
		cancels := make([]context.CancelFunc, 0, clients)
		recorders := make([]*realtimeLoadRecorder, 0, clients)
		done := make(chan struct{}, clients)
		for range clients {
			ctx, cancel := context.WithCancel(realtimeAuthorizedTestContext(t.Context(), head.scope))
			cancels = append(cancels, cancel)
			recorder := newRealtimeLoadRecorder()
			recorders = append(recorders, recorder)
			request := httptest.NewRequest(http.MethodGet, RealtimePath, nil).WithContext(ctx)
			go func() {
				defer func() { done <- struct{}{} }()
				route.Handler.ServeHTTP(recorder, request)
			}()
		}
		for _, recorder := range recorders {
			select {
			case <-recorder.ready:
			case <-time.After(3 * time.Second):
				t.Fatal("reconnect load client did not receive initial frames")
			}
		}
		if metrics := broadcaster.RealtimeBroadcasterMetrics(); metrics.ActiveClients != clients || metrics.ActiveTenants != 1 {
			t.Fatalf("wave %d did not share one tenant watcher: %+v", wave, metrics)
		}
		if got := head.calls.Load(); got != int32(wave) {
			t.Fatalf("wave %d audit queries=%d, want %d for %d clients", wave, got, wave, clients)
		}
		for _, cancel := range cancels {
			cancel()
		}
		for range clients {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("reconnect load handler did not stop")
			}
		}
		if metrics := broadcaster.RealtimeBroadcasterMetrics(); metrics.ActiveClients != 0 || metrics.ActiveTenants != 0 {
			t.Fatalf("wave %d retained broadcaster state: %+v", wave, metrics)
		}
	}
	if metrics := broadcaster.RealtimeBroadcasterMetrics(); metrics.AcceptedClients != clients*waves || metrics.PeakClients != clients || metrics.AuditHeadQueries != waves {
		t.Fatalf("unexpected reconnect load profile: %+v", metrics)
	}
}

func TestRealtimeTenantLimitReturns429BeforeStream(t *testing.T) {
	head := &a10AuditHead{scope: validTestScope(t)}
	head.id.Store("synthetic-head")
	routes, _ := newRealtimeRoutesWithBroadcaster(head, realtimeTiming{
		pollInterval:         time.Hour,
		heartbeatInterval:    time.Hour,
		writeTimeout:         time.Second,
		maxClientsPerTenant:  1,
		maxClientsPerProcess: 2,
	})
	ctx, cancel := context.WithCancel(realtimeAuthorizedTestContext(t.Context(), head.scope))
	first := newRealtimeLoadRecorder()
	done := make(chan struct{}, 1)
	go func() {
		defer func() { done <- struct{}{} }()
		routes[0].Handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, RealtimePath, nil).WithContext(ctx))
	}()
	select {
	case <-first.ready:
	case <-time.After(time.Second):
		t.Fatal("first SSE client did not connect")
	}

	rejected := &a10FailingStream{ResponseRecorder: httptest.NewRecorder()}
	routes[0].Handler.ServeHTTP(rejected, httptest.NewRequest(http.MethodGet, RealtimePath, nil).WithContext(realtimeAuthorizedTestContext(t.Context(), head.scope)))
	if rejected.Code != http.StatusTooManyRequests || rejected.Header().Get("Retry-After") == "" || strings.Contains(rejected.Header().Get("Content-Type"), "event-stream") {
		t.Fatalf("limit response status=%d headers=%v", rejected.Code, rejected.Header())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("accepted client did not stop")
	}
}
