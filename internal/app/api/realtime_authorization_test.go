package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

func realtimeAuthorizedTestContext(ctx context.Context, scope tenancy.Scope) context.Context {
	authn := a10Principal().(authnStub)
	p := authn.principal
	ctx = context.WithValue(ctx, requestScopeKey{}, scope)
	return context.WithValue(ctx, requestReauthorizationKey{}, requestReauthorization{
		deps:   securityDependencies{authenticator: authn, tenant: tenantStub{scope: scope}, authorizer: authzStub{}},
		issuer: p.Issuer, subject: p.Subject, expiresAt: p.ExpiresAt, scope: scope, permission: "operations.realtime.read",
	})
}

type realtimeAuthFunc func(context.Context, *http.Request) (Principal, error)

func (f realtimeAuthFunc) Authenticate(ctx context.Context, r *http.Request) (Principal, error) {
	return f(ctx, r)
}

type realtimeAuthorizeFunc func(context.Context, Principal, tenancy.Scope, string) error

func (f realtimeAuthorizeFunc) Authorize(ctx context.Context, p Principal, s tenancy.Scope, permission string) error {
	return f(ctx, p, s, permission)
}

func TestRealtimeAuthorizationUsesFreshContextAndOriginalPermission(t *testing.T) {
	scope := validTestScope(t)
	ctx := realtimeAuthorizedTestContext(t.Context(), scope)
	access := ctx.Value(requestReauthorizationKey{}).(requestReauthorization)
	fresh := access.deps.authenticator.(authnStub).principal
	fresh.Roles = []string{"viewer"}
	access.deps.authenticator = authnStub{principal: fresh}
	called := false
	access.deps.authorizer = realtimeAuthorizeFunc(func(ctx context.Context, p Principal, s tenancy.Scope, permission string) error {
		called = true
		fromContext, ok := PrincipalFromContext(ctx)
		resolved, scoped := ScopeFromContext(ctx)
		if !ok || !scoped || len(fromContext.Roles) != 1 || fromContext.Roles[0] != "viewer" || permission != "operations.realtime.read" || resolved != s || s != scope {
			t.Fatal("reauthorization reused stale context or another permission")
		}
		return nil
	})
	if err := access.check(ctx, httptest.NewRequest(http.MethodGet, RealtimePath, nil)); err != nil || !called {
		t.Fatal("current authorizer was not consulted", err)
	}
}

func TestRealtimeAuthorizationBoundsBlockedWriteAtExpiry(t *testing.T) {
	head := &a10AuditHead{scope: validTestScope(t)}
	head.id.Store("synthetic-head")
	p := a10Principal().(authnStub).principal
	p.ExpiresAt = time.Now().Add(500 * time.Millisecond)
	handler, done := a10Handler(t, head, realtimeTiming{pollInterval: time.Second, heartbeatInterval: time.Second, writeTimeout: 5 * time.Second}, authnStub{principal: p}, authzStub{})
	serverConn, clientConn := net.Pipe()
	listener := &a10PipeListener{conn: a10LocalConn{serverConn}, closed: make(chan struct{})}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second, WriteTimeout: time.Hour}
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
	r, _ := http.NewRequest(http.MethodGet, "http://synthetic.example.test"+RealtimePath, nil)
	if err := r.Write(clientConn); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(clientConn), r)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	a10Frame(t, bufio.NewReader(response.Body))
	// Stop reading, but leave the connection open. The credential deadline
	// must interrupt the blocked frame before its ordinary five-second budget.
	a10AwaitExit(t, done)
	if time.Now().Before(p.ExpiresAt) {
		t.Fatal("stream ended before credential expiry")
	}
}

func TestRealtimeAuthorizationRechecksBindingAndPermission(t *testing.T) {
	scope := validTestScope(t)
	otherScope, _ := tenancy.ParseScope(scope.OrganizationID().String(), "018f1c8a-7b3c-7def-8000-000000000003")
	base := Principal{Issuer: "synthetic-issuer", Subject: "synthetic-subject", SessionRef: "synthetic-session", SubjectRef: "synthetic-reference", ExpiresAt: time.Now().Add(time.Hour)}
	for _, tc := range []struct {
		name                              string
		change                            func(*Principal)
		scope                             tenancy.Scope
		authErr, tenantErr, permissionErr error
		allow                             bool
	}{
		{name: "unchanged", scope: scope, allow: true},
		{name: "fresh_permitted_role", scope: scope, change: func(p *Principal) { p.Roles = []string{"viewer"} }, allow: true},
		{name: "issuer", scope: scope, change: func(p *Principal) { p.Issuer = "other-issuer" }},
		{name: "subject", scope: scope, change: func(p *Principal) { p.Subject = "other-subject" }},
		{name: "session", scope: scope, change: func(p *Principal) { p.SessionRef = "other-session" }},
		{name: "subject_reference", scope: scope, change: func(p *Principal) { p.SubjectRef = "other-reference" }},
		{name: "extended_expiry", scope: scope, change: func(p *Principal) { p.ExpiresAt = p.ExpiresAt.Add(time.Hour) }},
		{name: "expired", scope: scope, change: func(p *Principal) { p.ExpiresAt = time.Now().Add(-time.Second) }},
		{name: "workspace", scope: otherScope},
		{name: "session_revoked", scope: scope, authErr: ErrUnauthenticated},
		{name: "session_store_unavailable", scope: scope, authErr: ErrAuthenticationUnavailable},
		{name: "member_disabled", scope: scope, tenantErr: ErrUnauthorized},
		{name: "membership_store_failure", scope: scope, tenantErr: errors.New("synthetic DB detail")},
		{name: "permission_removed", scope: scope, permissionErr: ErrUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := base
			if tc.change != nil {
				tc.change(&fresh)
			}
			access := requestReauthorization{
				deps:   securityDependencies{authenticator: authnStub{principal: fresh, err: tc.authErr}, tenant: tenantStub{scope: tc.scope, err: tc.tenantErr}, authorizer: authzStub{err: tc.permissionErr}},
				issuer: base.Issuer, subject: base.Subject, sessionRef: base.SessionRef, subjectRef: base.SubjectRef, expiresAt: base.ExpiresAt, scope: scope, permission: "operations.realtime.read",
			}
			err := access.check(t.Context(), httptest.NewRequest(http.MethodGet, RealtimePath, nil))
			if (err == nil) != tc.allow {
				t.Fatal("incorrect reauthorization result", err)
			}
			if err != nil && strings.Contains(err.Error(), "synthetic") {
				t.Fatal("dependency details leaked")
			}
		})
	}
}

func TestRealtimeAuthorizationExpiryIsPrivateAndRequired(t *testing.T) {
	p := Principal{Issuer: "issuer", Subject: "subject", ExpiresAt: time.Now().Add(time.Hour)}
	raw, err := json.Marshal(p)
	if err != nil || strings.Contains(string(raw), "ExpiresAt") || strings.Contains(string(raw), p.ExpiresAt.Format(time.RFC3339)) {
		t.Fatal("credential expiry became a public principal field")
	}
	for _, missing := range []bool{false, true} {
		scope := validTestScope(t)
		ctx := context.WithValue(t.Context(), requestScopeKey{}, scope)
		if !missing {
			ctx = realtimeAuthorizedTestContext(ctx, scope)
			access := ctx.Value(requestReauthorizationKey{}).(requestReauthorization)
			access.expiresAt = time.Time{}
			ctx = context.WithValue(ctx, requestReauthorizationKey{}, access)
		}
		head := &a10AuditHead{scope: scope}
		head.id.Store("synthetic-head")
		recorder := httptest.NewRecorder()
		newRealtimeRoutes(head)[0].Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, RealtimePath, nil).WithContext(ctx))
		if (recorder.Code != http.StatusUnauthorized && recorder.Code != http.StatusForbidden) || head.calls.Load() != 0 {
			t.Fatal("unbound or unbounded stream started")
		}
	}
}

func TestRealtimeAuthorizationExpiryAndUnavailableCloseHTTP(t *testing.T) {
	for _, tc := range []string{"expiry_http1", "expiry_http2", "provider_error", "check_timeout", "late_success", "cancel_during_check"} {
		t.Run(tc, func(t *testing.T) {
			p := a10Principal().(authnStub).principal
			timing := realtimeTiming{pollInterval: 20 * time.Millisecond, heartbeatInterval: 40 * time.Millisecond, writeTimeout: 100 * time.Millisecond, revalidateInterval: 80 * time.Millisecond, revalidateTimeout: 100 * time.Millisecond}
			if strings.HasPrefix(tc, "expiry") {
				p.ExpiresAt = time.Now().Add(300 * time.Millisecond)
				timing.revalidateInterval = time.Hour
			}
			var calls atomic.Int32
			checking, exited := make(chan struct{}), make(chan struct{})
			authn := realtimeAuthFunc(func(ctx context.Context, r *http.Request) (Principal, error) {
				if calls.Add(1) == 1 {
					return p, nil
				}
				close(checking)
				defer close(exited)
				if tc == "provider_error" {
					return Principal{}, ErrAuthenticationUnavailable
				}
				<-ctx.Done()
				if tc == "late_success" {
					return p, nil
				}
				return Principal{}, ctx.Err()
			})
			head := &a10AuditHead{scope: validTestScope(t)}
			head.id.Store("synthetic-head")
			handler, done := a10Handler(t, head, timing, authn, authzStub{})
			server := httptest.NewUnstartedServer(handler)
			server.EnableHTTP2 = tc == "expiry_http2"
			server.StartTLS()
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			r, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+RealtimePath, nil)
			response, err := server.Client().Do(r)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status=%d", response.StatusCode)
			}
			reader := bufio.NewReader(response.Body)
			a10Frame(t, reader)
			a10Frame(t, reader)
			if tc == "cancel_during_check" {
				select {
				case <-checking:
				case <-ctx.Done():
					t.Fatal("reauthorization did not start")
				}
				cancel()
			}
			_, err = io.Copy(io.Discard, reader)
			if tc != "cancel_during_check" && (err != nil || ctx.Err() != nil) {
				t.Fatal("stream did not end on lost authorization", err)
			}
			a10AwaitExit(t, done)
			if strings.HasPrefix(tc, "expiry") {
				if time.Now().Before(p.ExpiresAt) || calls.Load() != 1 {
					t.Fatal("expiry depends on periodic reauthorization")
				}
			} else {
				select {
				case <-exited:
				default:
					t.Fatal("security check outlived HTTP handler")
				}
				if calls.Load() != 2 {
					t.Fatal("failed authorization retried within the same stream")
				}
			}
		})
	}
}

type realtimeRevocableAuthorizer struct{ denied atomic.Bool }

func (a *realtimeRevocableAuthorizer) Authorize(ctx context.Context, principal Principal, scope tenancy.Scope, permission string) error {
	if a.denied.Load() {
		return ErrUnauthorized
	}
	return nil
}

func TestRealtimeAuthorizationClosesDeniedStream(t *testing.T) {
	head := &a10AuditHead{scope: validTestScope(t)}
	head.id.Store("synthetic-head")
	authz := &realtimeRevocableAuthorizer{}
	handler, done := a10Handler(t, head, realtimeTiming{pollInterval: 20 * time.Millisecond, heartbeatInterval: 100 * time.Millisecond, writeTimeout: 100 * time.Millisecond, revalidateInterval: 50 * time.Millisecond, revalidateTimeout: 200 * time.Millisecond}, a10Principal(), authz)
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+RealtimePath, nil)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	a10Frame(t, reader)
	a10Frame(t, reader)
	authz.denied.Store(true)
	_, err = io.Copy(io.Discard, reader)
	if err != nil || ctx.Err() != nil {
		t.Fatal("stream continued after authorization was revoked")
	}
	a10AwaitExit(t, done)
}
