package api

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/securitysettingsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/tenancyrepo"
	"github.com/torgnexa/torgnexa/internal/platform/securityedge"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
)

type realtimePostgresQueryCounter struct {
	repository *auditrepo.Repository
	calls      atomic.Int32
}

func (c *realtimePostgresQueryCounter) List(ctx context.Context, scope tenancy.Scope, limit int, cursor string) ([]audit.Record, string, error) {
	return c.repository.List(ctx, scope, limit, cursor)
}

func (c *realtimePostgresQueryCounter) LatestID(ctx context.Context, scope tenancy.Scope) (string, error) {
	c.calls.Add(1)
	return c.repository.LatestID(ctx, scope)
}

func TestRealtimePostgresReauthorization(t *testing.T) {
	for _, scenario := range []string{"active_then_revoke", "member_disabled", "membership_unavailable", "session_check_failure"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, db, admin, scope := auditPostgres(t)
			db.SetMaxOpenConns(2)
			sessions, _ := securitysettingsrepo.New(db)
			members, _ := tenancyrepo.New(db)
			audits, _ := auditrepo.New(db)
			fixture := newA02OIDCFixture(t, scope, sessions, a02EmailCase{email: a02InvitedEmail, verification: "true"})
			a02Invite(t, ctx, members, scope, a02InvitedEmail, "viewer")
			member, err := members.ResolveActiveMember(ctx, scope, tenancyrepo.MemberIdentity{SubjectRef: identityReference(fixture.authenticator.cfg.Issuer, a02Subject), VerifiedEmail: a02InvitedEmail})
			if err != nil {
				t.Fatal(err)
			}
			routes := newRealtimeRoutesWithTiming(audits, realtimeTiming{pollInterval: 20 * time.Millisecond, heartbeatInterval: 100 * time.Millisecond, writeTimeout: time.Second, revalidateInterval: 50 * time.Millisecond, revalidateTimeout: time.Second})
			finished := make(chan struct{}, 1)
			stream := routes[0].Handler
			routes[0].Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer func() { finished <- struct{}{} }()
				stream.ServeHTTP(w, r)
			})
			handler, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), fixture.authenticator, claimTenantResolver{memberships: members}, roleAuthorizer{memberships: members}, routes, nil)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(handler)
			defer server.Close()
			clientCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			request, _ := http.NewRequestWithContext(clientCtx, http.MethodGet, server.URL+RealtimePath, nil)
			request.Header.Set("Authorization", "Bearer "+fixture.token)
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("stream status=%d", response.StatusCode)
			}
			reader := bufio.NewReader(response.Body)
			a10Frame(t, reader)
			a10Frame(t, reader)
			// An active session survives several monitor runs, without duplicating
			// first-login evidence or changing its membership binding.
			for range 2 {
				if event, _ := a10Frame(t, reader); event != "heartbeat" {
					t.Fatal("active stream lost authorization")
				}
			}
			saved := a09Session(t, ctx, sessions, scope)
			a09EventCounts(t, ctx, sessions, scope, 1, 0)
			switch scenario {
			case "active_then_revoke":
				_, err = sessions.Revoke(ctx, scope, securitysettings.RevokeCommand{EventID: auditFixtureID(), SessionRef: saved.Ref, ActorID: "synthetic-admin", CorrelationID: "sse-revoke", OccurredAt: time.Now().UTC()})
			case "member_disabled":
				_, err = members.UpdateMember(ctx, scope, member.ID, "viewer", "disabled", auditFixtureID(), member.Version)
			case "membership_unavailable":
				// Removing SELECT from only the disposable application role makes
				// the next canonical membership lookup fail, not grant access.
				_, err = admin.ExecContext(ctx, `REVOKE SELECT ON workspace_members FROM audit_integration`)
				t.Cleanup(func() {
					_, e := admin.ExecContext(context.Background(), `GRANT SELECT ON workspace_members TO audit_integration`)
					if e != nil {
						t.Error(e)
					}
				})
			case "session_check_failure":
				_, err = admin.ExecContext(ctx, `REVOKE SELECT,UPDATE ON settings_identity_sessions FROM audit_integration`)
				t.Cleanup(func() {
					_, e := admin.ExecContext(context.Background(), `GRANT SELECT,UPDATE ON settings_identity_sessions TO audit_integration`)
					if e != nil {
						t.Error(e)
					}
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.Copy(io.Discard, reader)
			if err != nil || clientCtx.Err() != nil {
				t.Fatal("stream did not close after authoritative access changed", err)
			}
			a10AwaitExit(t, finished)
			if scenario == "active_then_revoke" {
				if item := a09Session(t, ctx, sessions, scope); item.Status != "revoked" {
					t.Fatal("monitor reactivated a revoked session")
				}
				a09EventCounts(t, ctx, sessions, scope, 1, 1)
			}
			if scenario == "member_disabled" {
				items, e := members.ListMembers(ctx, scope, "", 10)
				if e != nil || len(items) != 1 || items[0].Status != "disabled" {
					t.Fatal("monitor restored disabled membership", e)
				}
			}
		})
	}
}

func TestRealtimePostgresBroadcasterFanOutQueryCount(t *testing.T) {
	const clients = 32
	ctx, db, _, scope := auditPostgres(t)
	repository, err := auditrepo.New(db)
	if err != nil {
		t.Fatal(err)
	}
	counter := &realtimePostgresQueryCounter{repository: repository}
	routes, broadcaster := newRealtimeRoutesWithBroadcaster(counter, realtimeTiming{
		pollInterval:      time.Hour,
		heartbeatInterval: time.Hour,
		writeTimeout:      time.Second,
	})

	cancels := make([]context.CancelFunc, 0, clients)
	recorders := make([]*realtimeLoadRecorder, 0, clients)
	done := make(chan struct{}, clients)
	for range clients {
		clientContext, cancel := context.WithCancel(realtimeAuthorizedTestContext(ctx, scope))
		cancels = append(cancels, cancel)
		recorder := newRealtimeLoadRecorder()
		recorders = append(recorders, recorder)
		request := httptest.NewRequest(http.MethodGet, RealtimePath, nil).WithContext(clientContext)
		go func() {
			defer func() { done <- struct{}{} }()
			routes[0].Handler.ServeHTTP(recorder, request)
		}()
	}
	for _, recorder := range recorders {
		select {
		case <-recorder.ready:
		case <-time.After(3 * time.Second):
			t.Fatal("PostgreSQL fan-out client did not connect")
		}
	}
	if counter.calls.Load() != 1 {
		t.Fatalf("initial PostgreSQL audit-head queries=%d, want 1 for %d clients", counter.calls.Load(), clients)
	}

	service, err := audit.NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Capture(ctx, scope, audit.Entry{
		ActorID:       "synthetic-load-test",
		Source:        "api",
		Action:        "realtime.load_test",
		ResourceType:  "workspace",
		ResourceID:    scope.WorkspaceID().String(),
		CorrelationID: auditFixtureID(),
		Risk:          audit.RiskRead,
		Summary:       audit.Summary{"clients": clients},
	}); err != nil {
		t.Fatal(err)
	}
	broadcaster.mu.Lock()
	var watcher *realtimeTenantWatcher
	for _, candidate := range broadcaster.tenants {
		watcher = candidate
	}
	broadcaster.mu.Unlock()
	if watcher == nil {
		t.Fatal("tenant watcher disappeared during load")
	}
	broadcaster.poll(watcher, false)
	for _, recorder := range recorders {
		select {
		case <-recorder.invalidation:
		case <-time.After(3 * time.Second):
			t.Fatal("PostgreSQL audit invalidation did not fan out")
		}
	}
	if counter.calls.Load() != 2 {
		t.Fatalf("fan-out PostgreSQL audit-head queries=%d, want one initial and one changed-head query", counter.calls.Load())
	}
	metrics := broadcaster.RealtimeBroadcasterMetrics()
	if metrics.ActiveClients != clients || metrics.ActiveTenants != 1 || metrics.DeliveriesQueued != clients {
		t.Fatalf("unexpected PostgreSQL fan-out metrics: %+v", metrics)
	}

	for _, cancel := range cancels {
		cancel()
	}
	for range clients {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("PostgreSQL fan-out client did not stop")
		}
	}
}
