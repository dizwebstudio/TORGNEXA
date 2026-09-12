package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	txboundary "github.com/torgnexa/torgnexa/internal/platform/postgres/database"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/securitysettingsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/tenancyrepo"
	"github.com/torgnexa/torgnexa/internal/platform/securityedge"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
)

// Force every caller past its missing-row SELECT before any INSERT can finish.
// The shared lock in the trigger releases all INSERTs together, without sleeps
// deciding whether the race happened. Only this synthetic workspace is affected.
func a09InsertBarrier(t *testing.T, ctx context.Context, admin *sql.DB, scope tenancy.Scope) (func(int), func()) {
	t.Helper()
	const lockKey int64 = 90234009
	name := "a09_insert_" + strings.ReplaceAll(scope.WorkspaceID().String(), "-", "")
	if _, err := admin.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock_shared(%d); RETURN NEW; END $$`, name, lockKey)); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON settings_identity_sessions FOR EACH ROW WHEN (NEW.workspace_id='%s') EXECUTE FUNCTION %s()`, name, scope.WorkspaceID().String(), name)); err != nil {
		t.Fatal(err)
	}
	locker, err := admin.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locker.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		locker.Close()
		t.Fatal(err)
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		defer locker.Close()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked bool
		if err := locker.QueryRowContext(cleanupCtx, `SELECT pg_advisory_unlock($1)`, lockKey).Scan(&unlocked); err != nil || !unlocked {
			t.Errorf("release synthetic INSERT barrier: %v", err)
		}
	}
	t.Cleanup(func() {
		release()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP TRIGGER "+name+" ON settings_identity_sessions; DROP FUNCTION "+name+"()"); err != nil {
			t.Error(err)
		}
	})
	wait := func(expected int) {
		t.Helper()
		for {
			var waiting int
			if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND classid=0 AND objid=$1 AND NOT granted`, lockKey).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting == expected {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("INSERT barrier: waiting=%d want=%d: %v", waiting, expected, ctx.Err())
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
	return wait, release
}

func a09Handler(t *testing.T, authenticator Authenticator, memberships *tenancyrepo.Repository) http.Handler {
	t.Helper()
	route := ProtectedRoute{Method: http.MethodGet, Path: "/api/v1/private", Permission: "orders.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })}
	handler, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), authenticator, claimTenantResolver{memberships: memberships}, roleAuthorizer{memberships: memberships}, []ProtectedRoute{route}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func a09Session(t *testing.T, ctx context.Context, repo *securitysettingsrepo.Repository, scope tenancy.Scope) securitysettings.Session {
	t.Helper()
	items, cursor, err := repo.ListSessions(ctx, scope, 100, "")
	if err != nil || len(items) != 1 || cursor != "" {
		t.Fatalf("session count=%d, want one: %v", len(items), err)
	}
	return items[0]
}

func a09EventCounts(t *testing.T, ctx context.Context, repo *securitysettingsrepo.Repository, scope tenancy.Scope, observed, revoked int) {
	t.Helper()
	items, cursor, err := repo.ListLoginEvents(ctx, scope, 100, "")
	if err != nil || cursor != "" {
		t.Fatal("list session evidence", err)
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.EventType]++
	}
	if len(items) != observed+revoked || counts["session_observed"] != observed || counts["session_revoked"] != revoked {
		t.Fatalf("session evidence counts=%v, want observed=%d revoked=%d", counts, observed, revoked)
	}
}

func TestA09PostgresConcurrentOIDCAuthentication(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	db.SetMaxOpenConns(4)
	second, err := sql.Open("pgx", os.Getenv("TORGNEXA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close() })
	second.SetMaxOpenConns(4)
	sessions, _ := securitysettingsrepo.New(db)
	members, _ := tenancyrepo.New(db)
	fixture := newA02OIDCFixture(t, scope, sessions, a02EmailCase{email: a02InvitedEmail, verification: "true"})
	a02Invite(t, ctx, members, scope, a02InvitedEmail, "viewer")
	if _, err := members.ResolveActiveMember(ctx, scope, tenancyrepo.MemberIdentity{SubjectRef: identityReference(fixture.authenticator.cfg.Issuer, a02Subject), VerifiedEmail: a02InvitedEmail}); err != nil {
		t.Fatal(err)
	}
	otherSessions, _ := securitysettingsrepo.New(second)
	otherMembers, _ := tenancyrepo.New(second)
	otherAuth := *fixture.authenticator
	otherAuth.sessions = otherSessions
	handlers := []http.Handler{a09Handler(t, fixture.authenticator, members), a09Handler(t, &otherAuth, otherMembers)}
	batch := func() <-chan int {
		results := make(chan int, 8)
		start := make(chan struct{})
		for i := range 8 {
			go func() {
				<-start
				response := httptest.NewRecorder()
				handlers[i%2].ServeHTTP(response, fixture.request().WithContext(ctx))
				results <- response.Code
			}()
		}
		close(start)
		return results
	}
	assertBatch := func(results <-chan int, expected int) {
		t.Helper()
		for range 8 {
			select {
			case code := <-results:
				if code != expected {
					t.Errorf("parallel API status=%d want=%d", code, expected)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	wait, release := a09InsertBarrier(t, ctx, admin, scope)
	results := batch()
	wait(8)
	release()
	assertBatch(results, http.StatusNoContent)
	session := a09Session(t, ctx, sessions, scope)
	a09EventCounts(t, ctx, sessions, scope, 1, 0)
	for range 2 {
		assertBatch(batch(), http.StatusNoContent)
	}
	a09EventCounts(t, ctx, sessions, scope, 1, 0)
	if _, err := sessions.Revoke(ctx, scope, securitysettings.RevokeCommand{EventID: auditFixtureID(), SessionRef: session.Ref, ActorID: "synthetic-admin", CorrelationID: "a09-revoke", OccurredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	before := a09Session(t, ctx, sessions, scope)
	assertBatch(batch(), http.StatusUnauthorized)
	if after := a09Session(t, ctx, sessions, scope); !reflect.DeepEqual(before, after) || after.Status != "revoked" {
		t.Fatal("parallel authentication changed or reactivated the revoked session")
	}
	a09EventCounts(t, ctx, sessions, scope, 1, 1)
	if auditCount(t, ctx, admin, scope) != 1 {
		t.Fatal("session revocation lost or duplicated audit evidence")
	}
}

func a09Observation() securitysettings.Observation {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return securitysettings.Observation{EventID: auditFixtureID(), SessionRef: strings.Repeat("a", 64), SubjectRef: strings.Repeat("b", 64), ClientKind: "browser", AuthenticatedAt: now.Add(-time.Hour), ObservedAt: now, ExpiresAt: now.Add(time.Hour)}
}

func TestA09PostgresObserveSerializesWithRevocation(t *testing.T) {
	for _, revokeFirst := range []bool{true, false} {
		name := "observe_first"
		if revokeFirst {
			name = "revoke_first"
		}
		t.Run(name, func(t *testing.T) {
			ctx, db, admin, scope := auditPostgres(t)
			db.SetMaxOpenConns(2)
			repo, _ := securitysettingsrepo.New(db)
			initial := a09Observation()
			if err := repo.Observe(ctx, scope, initial); err != nil {
				t.Fatal(err)
			}
			next := initial
			next.EventID = auditFixtureID()
			next.ObservedAt = initial.ObservedAt.Add(time.Minute)
			next.ExpiresAt = initial.ExpiresAt.Add(time.Hour)
			command := securitysettings.RevokeCommand{EventID: auditFixtureID(), SessionRef: initial.SessionRef, ActorID: "synthetic-admin", CorrelationID: "a09-lock-order", OccurredAt: initial.ObservedAt.Add(2 * time.Minute)}
			observe := func(ctx context.Context) error { return repo.Observe(ctx, scope, next) }
			revoke := func(ctx context.Context) error { _, err := repo.Revoke(ctx, scope, command); return err }
			first, second := observe, revoke
			if revokeFirst {
				first, second = revoke, observe
			}
			commit := make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(commit) }) }
			t.Cleanup(release)
			locked := make(chan int, 1)
			firstDone := make(chan error, 1)
			go func() {
				firstDone <- txboundary.WithinTransaction(ctx, db, scope, func(bound context.Context) error {
					if err := first(bound); err != nil {
						return err
					}
					tx, err := txboundary.CurrentTransaction(bound, db)
					if err != nil {
						return err
					}
					var pid int
					if err := tx.QueryRowContext(bound, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
						return err
					}
					locked <- pid
					select {
					case <-commit:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			}()
			var holder int
			select {
			case holder = <-locked:
			case err := <-firstDone:
				t.Fatal("first transaction did not hold the row", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			secondDone := make(chan error, 1)
			go func() { secondDone <- second(ctx) }()
			for {
				var blocked bool
				if err := admin.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, holder).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				select {
				case err := <-secondDone:
					t.Fatal("second operation escaped row serialization", err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(5 * time.Millisecond):
				}
			}
			release()
			if err := <-firstDone; err != nil {
				t.Fatal(err)
			}
			err := <-secondDone
			if revokeFirst && !errors.Is(err, securitysettings.ErrSessionRevoked) || !revokeFirst && err != nil {
				t.Fatalf("second operation: %v", err)
			}
			saved := a09Session(t, ctx, repo, scope)
			want := next
			if revokeFirst {
				want = initial
			}
			if saved.Status != "revoked" || !saved.LastSeenAt.Equal(want.ObservedAt) || !saved.ExpiresAt.Equal(want.ExpiresAt) {
				t.Fatal("revocation or ordered observation state is incorrect")
			}
			if err := repo.Observe(ctx, scope, next); !errors.Is(err, securitysettings.ErrSessionRevoked) {
				t.Fatal("revoked session allowed a later observation", err)
			}
			if after := a09Session(t, ctx, repo, scope); !reflect.DeepEqual(saved, after) {
				t.Fatal("revoked session changed on retry")
			}
			a09EventCounts(t, ctx, repo, scope, 1, 1)
			if auditCount(t, ctx, admin, scope) != 1 {
				t.Fatal("revocation audit missing or duplicated")
			}
		})
	}
}

func TestA09PostgresCancelledInitialObservationRollsBack(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	repo, _ := securitysettingsrepo.New(db)
	value := a09Observation()
	alreadyCancelled, cancelBeforeObserve := context.WithCancel(ctx)
	cancelBeforeObserve()
	if err := repo.Observe(alreadyCancelled, scope, value); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled context was classified as invalid credentials", err)
	}
	wait, release := a09InsertBarrier(t, ctx, admin, scope)
	cancelCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- repo.Observe(cancelCtx, scope, value) }()
	wait(1)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled observation succeeded")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	release()
	items, _, err := repo.ListSessions(ctx, scope, 100, "")
	if err != nil || len(items) != 0 {
		t.Fatal("cancelled insertion left a session", err)
	}
	a09EventCounts(t, ctx, repo, scope, 0, 0)
	if err := repo.Observe(ctx, scope, value); err != nil {
		t.Fatal("retry after cancellation failed", err)
	}
	a09Session(t, ctx, repo, scope)
	a09EventCounts(t, ctx, repo, scope, 1, 0)
}

func TestA09PostgresLoginEventFailureRollsBackSession(t *testing.T) {
	ctx, db, admin, scope := auditPostgres(t)
	repo, _ := securitysettingsrepo.New(db)
	members, _ := tenancyrepo.New(db)
	fixture := newA02OIDCFixture(t, scope, repo, a02EmailCase{email: a02InvitedEmail, verification: "true"})
	a02Invite(t, ctx, members, scope, a02InvitedEmail, "viewer")
	if _, err := members.ResolveActiveMember(ctx, scope, tenancyrepo.MemberIdentity{SubjectRef: identityReference(fixture.authenticator.cfg.Issuer, a02Subject), VerifiedEmail: a02InvitedEmail}); err != nil {
		t.Fatal(err)
	}
	handler := a09Handler(t, fixture.authenticator, members)
	name := "a09_event_" + strings.ReplaceAll(scope.WorkspaceID().String(), "-", "")
	if _, err := admin.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic login event failure'; END $$`, name)); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON settings_login_events FOR EACH ROW WHEN (NEW.workspace_id='%s') EXECUTE FUNCTION %s()`, name, scope.WorkspaceID().String(), name)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.ExecContext(context.Background(), "DROP TRIGGER "+name+" ON settings_login_events; DROP FUNCTION "+name+"()")
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, fixture.request().WithContext(ctx))
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "5" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("session write failure should deny access with retryable 503: %d", response.Code)
	}
	if strings.Contains(response.Body.String(), "synthetic login event failure") || strings.Contains(response.Body.String(), fixture.token) {
		t.Fatal("session failure response leaked internal details")
	}
	items, _, err := repo.ListSessions(ctx, scope, 100, "")
	if err != nil || len(items) != 0 {
		t.Fatal("session survived its login event rollback", err)
	}
	a09EventCounts(t, ctx, repo, scope, 0, 0)
	if _, err := admin.ExecContext(ctx, "ALTER TABLE settings_login_events DISABLE TRIGGER "+name); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, fixture.request().WithContext(ctx))
		if response.Code != http.StatusNoContent {
			t.Fatalf("session write recovery/replay failed: %d", response.Code)
		}
	}
	a09Session(t, ctx, repo, scope)
	a09EventCounts(t, ctx, repo, scope, 1, 0)
}

func TestA09PostgresObservationPreservesBindingAndTenant(t *testing.T) {
	ctx, db, _, scope := auditPostgres(t)
	_, _, _, otherScope := auditPostgres(t)
	repo, _ := securitysettingsrepo.New(db)
	initial := a09Observation()
	if err := repo.Observe(ctx, scope, initial); err != nil {
		t.Fatal(err)
	}
	next := initial
	next.EventID = auditFixtureID()
	next.ObservedAt = initial.ObservedAt.Add(time.Minute)
	next.ExpiresAt = initial.ExpiresAt.Add(time.Hour)
	next.ClientKind = "mobile"
	next.AuthenticatedAt = initial.AuthenticatedAt.Add(time.Minute)
	if err := repo.Observe(ctx, scope, next); err != nil {
		t.Fatal(err)
	}
	if err := repo.Observe(ctx, scope, initial); err != nil {
		t.Fatal(err)
	}
	saved := a09Session(t, ctx, repo, scope)
	if !saved.LastSeenAt.Equal(next.ObservedAt) || !saved.ExpiresAt.Equal(next.ExpiresAt) || !saved.FirstSeenAt.Equal(initial.ObservedAt) || !saved.AuthenticatedAt.Equal(initial.AuthenticatedAt) || saved.ClientKind != initial.ClientKind || saved.SubjectRef != initial.SubjectRef {
		t.Fatal("observation regressed timestamps or changed immutable session metadata")
	}
	next.SubjectRef = strings.Repeat("c", 64)
	if err := repo.Observe(ctx, scope, next); !errors.Is(err, securitysettings.ErrInvalid) {
		t.Fatal("reused session reference accepted a different subject", err)
	}
	if after := a09Session(t, ctx, repo, scope); !reflect.DeepEqual(saved, after) {
		t.Fatal("mismatched subject changed session")
	}
	if err := repo.Observe(ctx, otherScope, next); err != nil {
		t.Fatal("independent workspace session rejected", err)
	}
	if other := a09Session(t, ctx, repo, otherScope); other.SubjectRef != next.SubjectRef {
		t.Fatal("session binding crossed workspace")
	}
	a09EventCounts(t, ctx, repo, scope, 1, 0)
	a09EventCounts(t, ctx, repo, otherScope, 1, 0)
}

func TestA09PostgresSessionLastSeenWriteIsThrottledButRevocationIsAuthoritative(t *testing.T) {
	ctx, db, _, scope := auditPostgres(t)
	repo, err := securitysettingsrepo.New(db)
	if err != nil {
		t.Fatal(err)
	}
	initial := a09Observation()
	result, err := repo.ObserveDetailed(ctx, scope, initial)
	if err != nil || !result.Created || !result.TimestampsUpdated {
		t.Fatalf("initial observation=%+v err=%v", result, err)
	}
	withinWindow := initial
	withinWindow.EventID = auditFixtureID()
	withinWindow.ObservedAt = initial.ObservedAt.Add(30 * time.Second)
	result, err = repo.ObserveDetailed(ctx, scope, withinWindow)
	if err != nil || result.Created || result.TimestampsUpdated {
		t.Fatalf("unthrottled observation=%+v err=%v", result, err)
	}
	if saved := a09Session(t, ctx, repo, scope); !saved.LastSeenAt.Equal(initial.ObservedAt) {
		t.Fatal("throttled observation changed last_seen_at")
	}
	afterWindow := withinWindow
	afterWindow.EventID = auditFixtureID()
	afterWindow.ObservedAt = initial.ObservedAt.Add(61 * time.Second)
	result, err = repo.ObserveDetailed(ctx, scope, afterWindow)
	if err != nil || result.Created || !result.TimestampsUpdated {
		t.Fatalf("coalesced observation=%+v err=%v", result, err)
	}
	if saved := a09Session(t, ctx, repo, scope); !saved.LastSeenAt.Equal(afterWindow.ObservedAt) {
		t.Fatal("last_seen_at did not advance after throttle interval")
	}
	if _, err := repo.Revoke(ctx, scope, securitysettings.RevokeCommand{EventID: auditFixtureID(), SessionRef: initial.SessionRef, ActorID: "synthetic-admin", CorrelationID: "last-seen-revoke", OccurredAt: afterWindow.ObservedAt.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ObserveDetailed(ctx, scope, afterWindow); !errors.Is(err, securitysettings.ErrSessionRevoked) {
		t.Fatal("throttled write path bypassed revocation", err)
	}
}

func TestA09PostgresAuthenticatedHotPathLoadMetrics(t *testing.T) {
	const requests = 32
	ctx, db, _, scope := auditPostgres(t)
	db.SetMaxOpenConns(8)
	sessions, err := securitysettingsrepo.New(db)
	if err != nil {
		t.Fatal(err)
	}
	members, err := tenancyrepo.New(db)
	if err != nil {
		t.Fatal(err)
	}
	a02Invite(t, ctx, members, scope, a02InvitedEmail, "viewer")
	fixture := newA02OIDCFixture(t, scope, sessions, a02EmailCase{email: a02InvitedEmail, verification: "true"})
	cache := newMembershipCache(members, fixture.authenticator.metrics)
	route := ProtectedRoute{Method: http.MethodGet, Path: "/api/v1/private", Permission: "orders.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })}
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
			handler.ServeHTTP(response, fixture.request().WithContext(ctx))
			statuses <- response.Code
		}()
	}
	close(start)
	wait.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusNoContent {
			t.Fatalf("authenticated PostgreSQL load status=%d", status)
		}
	}
	metrics := fixture.authenticator.OIDCHotPathMetrics()
	if metrics.Requests != requests || metrics.Authorized != requests || metrics.JWKSHTTPCalls != 1 || metrics.UserInfoHTTPCalls != 1 ||
		metrics.SessionDBCalls != requests || metrics.SessionLastSeenWrites != 1 || metrics.SessionWritesThrottled != requests-1 || metrics.MembershipDBCalls != 1 ||
		metrics.IdPCallsPerRequest.TotalCalls != 2 || metrics.DBCallsPerRequest.TotalCalls != requests+1 || metrics.Latency.Count != requests || metrics.Latency.P99 <= 0 {
		t.Fatalf("PostgreSQL hot-path metrics=%+v", metrics)
	}
	a09Session(t, ctx, sessions, scope)
	a09EventCounts(t, ctx, sessions, scope, 1, 0)
}
