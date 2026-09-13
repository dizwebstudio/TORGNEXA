package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/securityedge"
)

// The fixture carries no audit payload and checks the scope passed by the real
// protected-route composition. Atomic values allow mutations during a stream.
type a10AuditHead struct {
	scope tenancy.Scope
	id    atomic.Value
	calls atomic.Int32
}

func TestA10RealtimeKeepsOrdinaryTimeoutAndAuthorization(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		t.Run(fmt.Sprintf("http2=%t", http2), func(t *testing.T) {
			head := &a10AuditHead{scope: validTestScope(t)}
			head.id.Store("synthetic-head")
			flushed := make(chan error, 1)
			extra := ProtectedRoute{Method: http.MethodGet, Path: "/api/v1/slow", Permission: "orders.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				time.Sleep(200 * time.Millisecond)
				_, _ = io.WriteString(w, "synthetic response after timeout")
				flushed <- http.NewResponseController(w).Flush()
			})}
			handler, _ := a10Handler(t, head, realtimeTiming{pollInterval: time.Second, heartbeatInterval: time.Second, writeTimeout: time.Second}, a10Principal(), authzStub{}, extra)
			server := httptest.NewUnstartedServer(handler)
			server.Config.WriteTimeout = 50 * time.Millisecond
			server.EnableHTTP2 = http2
			server.StartTLS()
			t.Cleanup(server.Close)
			client := server.Client()
			client.Timeout = time.Second
			response, err := client.Get(server.URL + extra.Path)
			if response != nil {
				response.Body.Close()
			}
			if err == nil {
				t.Fatal("ordinary route survived its configured write timeout")
			}
			select {
			case err := <-flushed:
				if err == nil {
					t.Fatal("ordinary response flush ignored the configured timeout")
				}
			case <-time.After(time.Second):
				t.Fatal("ordinary handler did not complete")
			}
			if head.calls.Load() != 0 {
				t.Fatal("ordinary route entered the SSE handler")
			}
		})
	}
	for _, tc := range []struct {
		name  string
		authn Authenticator
		authz Authorizer
		want  int
	}{
		{"unauthenticated", authnStub{err: ErrUnauthenticated}, authzStub{}, http.StatusUnauthorized},
		{"forbidden", a10Principal(), authzStub{err: ErrUnauthorized}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			head := &a10AuditHead{scope: validTestScope(t)}
			head.id.Store("synthetic-head")
			handler, _ := a10Handler(t, head, realtimeTiming{pollInterval: time.Second, heartbeatInterval: time.Second, writeTimeout: time.Second}, tc.authn, tc.authz)
			server := httptest.NewServer(handler)
			t.Cleanup(server.Close)
			response, err := server.Client().Get(server.URL + RealtimePath)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.want || strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") || head.calls.Load() != 0 {
				t.Fatal("SSE bypassed the protected-route composition")
			}
		})
	}
}

// A single synchronous connection makes backpressure deterministic: once the
// client stops reading, a real net/http Flush must block until its deadline.
type a10PipeListener struct {
	conn   net.Conn
	once   sync.Once
	closed chan struct{}
	stop   sync.Once
}

func (l *a10PipeListener) Accept() (net.Conn, error) {
	var conn net.Conn
	l.once.Do(func() { conn = l.conn })
	if conn != nil {
		return conn, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}

func (l *a10PipeListener) Close() error {
	l.stop.Do(func() { close(l.closed) })
	return nil
}

func (l *a10PipeListener) Addr() net.Addr { return l.conn.LocalAddr() }

// net.Pipe has no IP address, so only its synthetic peer address is adapted to
// satisfy the actual edge policy. Writes/deadlines remain real net.Pipe I/O.
type a10LocalConn struct{ net.Conn }

func (c a10LocalConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 23410}
}

func TestA10RealtimeSlowClientReleasesHandler(t *testing.T) {
	head := &a10AuditHead{scope: validTestScope(t)}
	head.id.Store("synthetic-head")
	timing := realtimeTiming{pollInterval: 20 * time.Millisecond, heartbeatInterval: 300 * time.Millisecond, writeTimeout: 120 * time.Millisecond}
	handler, done := a10Handler(t, head, timing, a10Principal(), authzStub{})
	serverConn, clientConn := net.Pipe()
	listener := &a10PipeListener{conn: a10LocalConn{serverConn}, closed: make(chan struct{})}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second, WriteTimeout: time.Second, ErrorLog: nil}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		clientConn.Close()
		server.Close()
		if err := <-served; !errors.Is(err, http.ErrServerClosed) {
			t.Error(err)
		}
	})
	if err := clientConn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodGet, "http://synthetic.example.test"+RealtimePath, nil)
	if err := request.Write(clientConn); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(clientConn), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("SSE status=%d", response.StatusCode)
	}
	if event, _ := a10Frame(t, bufio.NewReader(response.Body)); event != "ready" {
		t.Fatal("missing ready frame before backpressure")
	}
	// Do not close/cancel/read the connection. The next frame must time out on
	// its own; the handler must not remain stuck in Flush. The tenant watcher
	// may poll independently while the frame is blocked, but it must stop when
	// the last subscriber is released.
	a10AwaitExit(t, done)
	calls := head.calls.Load()
	if calls < 1 || calls > 10 {
		t.Fatalf("unexpected audit-head query count during bounded slow write: %d", calls)
	}
	time.Sleep(3 * timing.pollInterval)
	stoppedCalls := head.calls.Load()
	time.Sleep(3 * timing.pollInterval)
	if head.calls.Load() != stoppedCalls {
		t.Fatal("failed flush retained the tenant watcher after its last client stopped")
	}
}

func (s *a10AuditHead) List(context.Context, tenancy.Scope, int, string) ([]audit.Record, string, error) {
	return nil, "", errors.New("realtime must use the metadata-only lookup")
}

func (s *a10AuditHead) LatestID(ctx context.Context, scope tenancy.Scope) (string, error) {
	s.calls.Add(1)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if scope.OrganizationID() != s.scope.OrganizationID() || scope.WorkspaceID() != s.scope.WorkspaceID() {
		return "", tenancy.ErrInvalidScope
	}
	return s.id.Load().(string), nil
}

func a10Handler(t *testing.T, head *a10AuditHead, timing realtimeTiming, authn Authenticator, authz Authorizer, extra ...ProtectedRoute) (http.Handler, <-chan struct{}) {
	t.Helper()
	routes := newRealtimeRoutesWithTiming(head, timing)
	done := make(chan struct{}, 16)
	stream := routes[0].Handler
	routes[0].Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { done <- struct{}{} }()
		stream.ServeHTTP(w, r)
	})
	handler, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), authn, tenantStub{scope: head.scope}, authz, append(routes, extra...), nil)
	if err != nil {
		t.Fatal(err)
	}
	return handler, done
}

func a10Principal() Authenticator {
	return authnStub{principal: Principal{Issuer: "https://synthetic.example.test", Subject: "synthetic-user", ExpiresAt: time.Now().Add(time.Hour)}}
}

func a10Frame(t *testing.T, reader *bufio.Reader) (string, realtimeEvent) {
	t.Helper()
	var name string
	var value realtimeEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE frame: %v", err)
		}
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			return name, value
		}
		if raw, ok := strings.CutPrefix(line, "event: "); ok {
			name = raw
		}
		if raw, ok := strings.CutPrefix(line, "data: "); ok {
			decoder := json.NewDecoder(strings.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&value); err != nil {
				t.Fatal(err)
			}
			if _, err := time.Parse(time.RFC3339Nano, value.At); err != nil || !strings.HasSuffix(value.At, "Z") {
				t.Fatal("SSE timestamp is not UTC RFC3339")
			}
		}
	}
}

func a10AwaitExit(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not release the stream")
	}
}

func TestA10RealtimeHTTPDeadlineAndReconnect(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		name := "http1"
		if http2 {
			name = "http2"
		}
		t.Run(name, func(t *testing.T) {
			head := &a10AuditHead{scope: validTestScope(t)}
			head.id.Store("synthetic-audit-before")
			timing := realtimeTiming{pollInterval: 20 * time.Millisecond, heartbeatInterval: 300 * time.Millisecond, writeTimeout: 100 * time.Millisecond}
			handler, done := a10Handler(t, head, timing, a10Principal(), authzStub{})
			server := httptest.NewUnstartedServer(handler)
			server.Config.WriteTimeout = 80 * time.Millisecond
			server.EnableHTTP2 = http2
			server.StartTLS()
			t.Cleanup(server.Close)
			client := server.Client()
			client.Timeout = 3 * time.Second
			connect := func() (*http.Response, *bufio.Reader, context.CancelFunc) {
				ctx, cancel := context.WithCancel(t.Context())
				t.Cleanup(cancel)
				request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+RealtimePath, nil)
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { response.Body.Close() })
				if response.StatusCode != http.StatusOK || (response.ProtoMajor == 2) != http2 {
					t.Fatalf("unexpected SSE response: %s %d", response.Proto, response.StatusCode)
				}
				return response, bufio.NewReader(response.Body), cancel
			}
			_, reader, cancel := connect()
			if event, value := a10Frame(t, reader); event != "ready" || value.Cursor != "synthetic-audit-before" {
				t.Fatal("missing initial ready/baseline")
			}
			if event, value := a10Frame(t, reader); event != "invalidate" || value.Reason != "connected" {
				t.Fatal("connection did not request a cache refresh")
			}
			// Each idle interval exceeds both the ordinary HTTP timeout and the
			// per-frame deadline: neither may remain armed between frames.
			for range 2 {
				if event, _ := a10Frame(t, reader); event != "heartbeat" {
					t.Fatal("stream did not survive an idle heartbeat interval")
				}
			}
			head.id.Store("synthetic-audit-online")
			if event, value := a10Frame(t, reader); event != "invalidate" || value.Reason != "audit" || value.Cursor != "synthetic-audit-online" {
				t.Fatal("audit change was lost after the ordinary HTTP timeout")
			}
			cancel()
			a10AwaitExit(t, done)
			for _, baseline := range []string{"synthetic-audit-during-disconnect", "synthetic-audit-during-disconnect", ""} {
				head.id.Store(baseline)
				_, reader, cancel = connect()
				if event, value := a10Frame(t, reader); event != "ready" || value.Cursor != baseline {
					t.Fatal("reconnect did not establish the current baseline")
				}
				if event, value := a10Frame(t, reader); event != "invalidate" || value.Reason != "connected" || value.Cursor != baseline {
					t.Fatal("reconnect silently swallowed changes made during the gap")
				}
				cancel()
				a10AwaitExit(t, done)
			}
		})
	}
}
