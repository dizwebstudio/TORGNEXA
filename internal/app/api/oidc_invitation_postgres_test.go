package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/securitysettingsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/tenancyrepo"
	"github.com/torgnexa/torgnexa/internal/platform/securityedge"
)

func a02ProtectedRequest(t *testing.T, ctx context.Context, fixture *a02OIDCFixture, members *tenancyrepo.Repository, permission string) int {
	t.Helper()
	called := false
	route := ProtectedRoute{Method: http.MethodGet, Path: "/api/v1/private", Permission: permission, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})}
	handler, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), fixture.authenticator, claimTenantResolver{memberships: members}, roleAuthorizer{memberships: members}, []ProtectedRoute{route}, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, fixture.request().WithContext(ctx))
	if called != (response.Code == http.StatusNoContent) {
		t.Fatal("privileged handler ran on rejected request")
	}
	for _, sensitive := range []string{a02InvitedEmail, a02Subject, fixture.token, "oidc_subject", "VerifiedEmail"} {
		if strings.Contains(response.Body.String(), sensitive) {
			t.Fatal("invitation/auth response exposed identity evidence")
		}
	}
	return response.Code
}

func a02Invite(t *testing.T, ctx context.Context, repo *tenancyrepo.Repository, scope tenancy.Scope, email, role string) tenancyrepo.Member {
	t.Helper()
	member, err := repo.InviteMember(ctx, scope, tenancyrepo.Member{ID: auditFixtureID(), Email: email, DisplayName: "Synthetic invitation", Role: role, InvitationKey: auditFixtureID()})
	if err != nil {
		t.Fatal(err)
	}
	return member
}

func a02AssertUnchanged(t *testing.T, ctx context.Context, repo *tenancyrepo.Repository, scope tenancy.Scope, before tenancyrepo.Member) {
	t.Helper()
	after, err := repo.GetMember(ctx, scope, before.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("rejected identity changed invitation/member: %v", err)
	}
}

func TestA02PostgresInvitationOwnershipAndReplay(t *testing.T) {
	for _, emailCase := range a02EmailCases() {
		t.Run(emailCase.name, func(t *testing.T) {
			ctx, db, _, scope := auditPostgres(t)
			members, err := tenancyrepo.New(db)
			if err != nil {
				t.Fatal(err)
			}
			sessions, err := securitysettingsrepo.New(db)
			if err != nil {
				t.Fatal(err)
			}
			invitation := a02Invite(t, ctx, members, scope, a02InvitedEmail, "admin")
			fixture := newA02OIDCFixture(t, scope, sessions, emailCase)
			want := http.StatusForbidden
			if emailCase.unauthenticated {
				want = http.StatusUnauthorized
			} else if emailCase.wantVerified == a02InvitedEmail {
				want = http.StatusNoContent
			}
			for range 2 {
				if code := a02ProtectedRequest(t, ctx, fixture, members, "settings.members.write"); code != want {
					t.Fatalf("invitation access status=%d want=%d", code, want)
				}
			}
			if want != http.StatusNoContent {
				a02AssertUnchanged(t, ctx, members, scope, invitation)
			}
			// A rejected attempt never consumes the invitation: only a subsequent
			// trusted ownership assertion may activate it, exactly once.
			fixture.setInfo(t, a02EmailCase{email: a02InvitedEmail, verification: "true"})
			for range 2 {
				if code := a02ProtectedRequest(t, ctx, fixture, members, "settings.members.write"); code != http.StatusNoContent {
					t.Fatalf("verified invitation/replay status=%d", code)
				}
			}
			saved, err := members.GetMember(ctx, scope, invitation.ID)
			if err != nil || saved.Status != "active" || saved.Role != "admin" || saved.Version != invitation.Version+1 || saved.OIDCSubject != identityReference(fixture.authenticator.cfg.Issuer, a02Subject) {
				t.Fatalf("verified binding or replay state invalid: %v", err)
			}
		})
	}
}

func TestA02PostgresExistingMemberCannotClaimAnotherRole(t *testing.T) {
	for _, verification := range []string{"true", "false", ""} {
		name := verification
		if name == "" {
			name = "missing"
		}
		t.Run(name, func(t *testing.T) {
			ctx, db, _, scope := auditPostgres(t)
			members, err := tenancyrepo.New(db)
			if err != nil {
				t.Fatal(err)
			}
			sessions, err := securitysettingsrepo.New(db)
			if err != nil {
				t.Fatal(err)
			}
			fixture := newA02OIDCFixture(t, scope, sessions, a02EmailCase{email: a02InvitedEmail, verification: verification})
			a02Invite(t, ctx, members, scope, "existing-viewer@example.test", "viewer")
			viewer, err := members.ResolveActiveMember(ctx, scope, tenancyrepo.MemberIdentity{SubjectRef: identityReference(fixture.authenticator.cfg.Issuer, a02Subject), VerifiedEmail: "existing-viewer@example.test"})
			if err != nil {
				t.Fatal(err)
			}
			adminInvitation := a02Invite(t, ctx, members, scope, a02InvitedEmail, "admin")
			if code := a02ProtectedRequest(t, ctx, fixture, members, "orders.read"); code != http.StatusNoContent {
				t.Fatalf("already bound viewer cannot read: %d", code)
			}
			if code := a02ProtectedRequest(t, ctx, fixture, members, "settings.members.write"); code != http.StatusForbidden {
				t.Fatalf("viewer acquired invited admin role: %d", code)
			}
			a02AssertUnchanged(t, ctx, members, scope, viewer)
			a02AssertUnchanged(t, ctx, members, scope, adminInvitation)
			// Disabled subjects must stay denied even when another invitation's
			// email matches; unique binding and the active-status check still apply.
			viewer, err = members.UpdateMember(ctx, scope, viewer.ID, "viewer", "disabled", "disable-viewer", viewer.Version)
			if err != nil {
				t.Fatal(err)
			}
			if code := a02ProtectedRequest(t, ctx, fixture, members, "orders.read"); code != http.StatusForbidden {
				t.Fatalf("disabled member regained access: %d", code)
			}
			a02AssertUnchanged(t, ctx, members, scope, viewer)
			a02AssertUnchanged(t, ctx, members, scope, adminInvitation)
		})
	}
}

func TestA02PostgresInvitationCannotCrossWorkspace(t *testing.T) {
	ctx, db, _, scope := auditPostgres(t)
	_, _, _, otherScope := auditPostgres(t)
	members, err := tenancyrepo.New(db)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := securitysettingsrepo.New(db)
	if err != nil {
		t.Fatal(err)
	}
	invitation := a02Invite(t, ctx, members, otherScope, a02InvitedEmail, "admin")
	fixture := newA02OIDCFixture(t, scope, sessions, a02EmailCase{email: a02InvitedEmail, verification: "true"})
	if code := a02ProtectedRequest(t, ctx, fixture, members, "settings.members.write"); code != http.StatusForbidden {
		t.Fatalf("verified email claimed another workspace invitation: %d", code)
	}
	a02AssertUnchanged(t, ctx, members, otherScope, invitation)
}
