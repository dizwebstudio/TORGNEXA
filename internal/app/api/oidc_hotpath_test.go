package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/tenancyrepo"
	"github.com/torgnexa/torgnexa/internal/platform/securityedge"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
)

type hotPathSessionStore struct {
	settingsSecurityStoreStub
	calls   atomic.Int64
	writes  atomic.Int64
	revoked atomic.Bool
}

func (store *hotPathSessionStore) Observe(ctx context.Context, scope tenancy.Scope, observation securitysettings.Observation) error {
	_, err := store.ObserveDetailed(ctx, scope, observation)
	return err
}

func (store *hotPathSessionStore) ObserveDetailed(context.Context, tenancy.Scope, securitysettings.Observation) (securitysettings.ObservationResult, error) {
	call := store.calls.Add(1)
	if store.revoked.Load() {
		return securitysettings.ObservationResult{}, securitysettings.ErrSessionRevoked
	}
	written := call == 1
	if written {
		store.writes.Add(1)
	}
	return securitysettings.ObservationResult{Created: written, TimestampsUpdated: written}, nil
}

type hotPathMembershipStore struct {
	member tenancyrepo.Member
	calls  atomic.Int64
}

func (store *hotPathMembershipStore) ResolveActiveMember(context.Context, tenancy.Scope, tenancyrepo.MemberIdentity) (tenancyrepo.Member, error) {
	store.calls.Add(1)
	return store.member, nil
}

func (store *hotPathMembershipStore) BootstrapDevelopmentAdministrator(context.Context, tenancy.Scope, string, string) (tenancyrepo.Member, error) {
	return tenancyrepo.Member{}, tenancyrepo.ErrMemberNotFound
}

func TestA09OIDCHotPathLoadProfileUsesLocalCachesAndOneMembership(t *testing.T) {
	const requests = 100
	scope := validTestScope(t)
	sessions := &hotPathSessionStore{}
	fixture := newA02OIDCFixture(t, scope, sessions, a02EmailCase{email: a02InvitedEmail, verification: "true"})
	members := &hotPathMembershipStore{member: tenancyrepo.Member{ID: "member-1", OIDCSubject: identityReference(fixture.authenticator.cfg.Issuer, a02Subject), Role: "viewer", Status: "active", Version: 1}}
	cache := newMembershipCache(members, fixture.authenticator.metrics)
	routeCalls := atomic.Int64{}
	route := ProtectedRoute{Method: http.MethodGet, Path: "/api/v1/private", Permission: "orders.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		routeCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})}
	handler, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), fixture.authenticator, claimTenantResolver{memberships: members, cache: cache}, roleAuthorizer{memberships: members, cache: cache}, []ProtectedRoute{route}, nil)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	statuses := make(chan int, requests)
	var wait sync.WaitGroup
	for range requests {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, fixture.request())
			statuses <- response.Code
		}()
	}
	close(start)
	wait.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusNoContent {
			t.Fatalf("authenticated load status=%d", status)
		}
	}
	if routeCalls.Load() != requests || sessions.calls.Load() != requests || sessions.writes.Load() != 1 || members.calls.Load() != 1 {
		t.Fatalf("calls: route=%d session=%d writes=%d membership=%d", routeCalls.Load(), sessions.calls.Load(), sessions.writes.Load(), members.calls.Load())
	}
	metrics := fixture.authenticator.OIDCHotPathMetrics()
	if metrics.Requests != requests || metrics.Authorized != requests || metrics.Denied != 0 || metrics.Unavailable != 0 ||
		metrics.JWKSHTTPCalls != 1 || metrics.UserInfoHTTPCalls != 1 || metrics.SessionDBCalls != requests || metrics.SessionLastSeenWrites != 1 || metrics.SessionWritesThrottled != requests-1 ||
		metrics.MembershipDBCalls != 1 || metrics.IdPCallsPerRequest.TotalCalls != 2 || metrics.IdPCallsPerRequest.MaximumCalls > 2 || metrics.DBCallsPerRequest.TotalCalls != requests+1 || metrics.DBCallsPerRequest.MaximumCalls > 2 ||
		metrics.Latency.Count != requests || metrics.Latency.Buckets[len(metrics.Latency.Buckets)-1].Count != requests || metrics.Latency.P50 <= 0 || metrics.Latency.P95 < metrics.Latency.P50 || metrics.Latency.P99 < metrics.Latency.P95 {
		t.Fatalf("unexpected hot-path metrics: %+v", metrics)
	}
	if fixture.jwksCalls.Load() != 1 || fixture.userinfoCalls.Load() != 1 {
		t.Fatalf("provider calls: jwks=%d userinfo=%d", fixture.jwksCalls.Load(), fixture.userinfoCalls.Load())
	}

	// Application session revocation remains authoritative on every request,
	// regardless of warm JWKS, profile and membership caches.
	sessions.revoked.Store(true)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, fixture.request())
	if response.Code != http.StatusUnauthorized || routeCalls.Load() != requests {
		t.Fatalf("revoked cached session status=%d routeCalls=%d", response.Code, routeCalls.Load())
	}
}

func TestOIDCHotPathLatencyStopsBeforeBusinessHandler(t *testing.T) {
	scope := validTestScope(t)
	sessions := &hotPathSessionStore{}
	fixture := newA02OIDCFixture(t, scope, sessions, a02EmailCase{email: a02InvitedEmail, verification: "true"})
	members := &hotPathMembershipStore{member: tenancyrepo.Member{ID: "member-1", OIDCSubject: identityReference(fixture.authenticator.cfg.Issuer, a02Subject), Role: "viewer", Status: "active", Version: 1}}
	cache := newMembershipCache(members, fixture.authenticator.metrics)
	started := make(chan struct{})
	release := make(chan struct{})
	route := ProtectedRoute{Method: http.MethodGet, Path: "/api/v1/private", Permission: "orders.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})}
	handler, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), fixture.authenticator, claimTenantResolver{memberships: members, cache: cache}, roleAuthorizer{memberships: members, cache: cache}, []ProtectedRoute{route}, nil)
	if err != nil {
		t.Fatal(err)
	}

	status := make(chan int, 1)
	go func() {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, fixture.request())
		status <- response.Code
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("business handler did not start")
	}
	metrics := fixture.authenticator.OIDCHotPathMetrics()
	if metrics.Requests != 1 || metrics.Authorized != 1 || metrics.Latency.Count != 1 {
		close(release)
		t.Fatalf("hot-path metrics were deferred until the business handler completed: %+v", metrics)
	}
	close(release)
	if got := <-status; got != http.StatusNoContent {
		t.Fatalf("handler status=%d", got)
	}
}

func TestOIDCWarmCachesSurviveProviderOutageAndColdProfileDegradesSafely(t *testing.T) {
	scope := validTestScope(t)
	sessions := &hotPathSessionStore{}
	fixture := newA02OIDCFixture(t, scope, sessions, a02EmailCase{email: a02InvitedEmail, verification: "true"})
	principal, err := fixture.authenticator.Authenticate(t.Context(), fixture.request())
	if err != nil || principal.VerifiedEmail != a02InvitedEmail {
		t.Fatal("initial hydration failed", err)
	}
	fixture.providerDown.Store(true)
	principal, err = fixture.authenticator.Authenticate(t.Context(), fixture.request())
	if err != nil || !principal.Valid() || fixture.jwksCalls.Load() != 1 || fixture.userinfoCalls.Load() != 1 {
		t.Fatalf("warm local authentication used unavailable provider: principal=%+v err=%v jwks=%d userinfo=%d", principal, err, fixture.jwksCalls.Load(), fixture.userinfoCalls.Load())
	}

	// A cold optional profile cache must not turn UserInfo availability into an
	// authentication dependency. Invitation proof stays empty until UserInfo is
	// successfully hydrated; token profile claims may still populate display.
	fixture.authenticator.profiles = newOIDCProfileCache(fixture.authenticator.metrics)
	principal, err = fixture.authenticator.Authenticate(t.Context(), fixture.request())
	if err != nil || !principal.Valid() || principal.VerifiedEmail != "" || principal.Profile.Email != a02InvitedEmail {
		t.Fatalf("cold profile outage was not safely degraded: principal=%+v err=%v", principal, err)
	}
	if fixture.userinfoCalls.Load() != 1 {
		t.Fatal("provider fixture counted rejected requests as successful UserInfo hydration")
	}
}

func TestOIDCColdJWKSOutageReturnsAuthenticationUnavailable(t *testing.T) {
	fixture := newA02OIDCFixture(t, validTestScope(t), &hotPathSessionStore{}, a02EmailCase{email: a02InvitedEmail, verification: "true"})
	fixture.providerDown.Store(true)
	if principal, err := fixture.authenticator.Authenticate(t.Context(), fixture.request()); !errors.Is(err, ErrAuthenticationUnavailable) || principal.Valid() {
		t.Fatalf("cold JWKS outage: principal=%+v err=%v", principal, err)
	}
}

func TestMembershipCacheHasBoundedStalenessAndRealtimeBypass(t *testing.T) {
	scope := validTestScope(t)
	store := &membershipStub{member: tenancyrepo.Member{Role: "viewer", Status: "active"}}
	metrics := &oidcHotPathRecorder{}
	cache := newMembershipCache(store, metrics)
	now := time.Now().UTC()
	cache.now = func() time.Time { return now }
	identity := tenancyrepo.MemberIdentity{SubjectRef: "synthetic-reference"}
	if _, err := cache.resolve(t.Context(), scope, identity); err != nil {
		t.Fatal(err)
	}
	store.err = tenancyrepo.ErrMemberNotFound
	if _, err := cache.resolve(t.Context(), scope, identity); err != nil {
		t.Fatal("membership cache expired before its bound", err)
	}
	now = now.Add(membershipCacheTTL + time.Nanosecond)
	if _, err := cache.resolve(t.Context(), scope, identity); !errors.Is(err, tenancyrepo.ErrMemberNotFound) {
		t.Fatal("membership cache exceeded bounded staleness", err)
	}
	if metrics.snapshot().MembershipDBCalls != 2 {
		t.Fatalf("membership DB calls=%d", metrics.snapshot().MembershipDBCalls)
	}
}
