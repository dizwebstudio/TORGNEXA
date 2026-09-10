package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/config"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/tenancyrepo"
)

type membershipStub struct {
	member         tenancyrepo.Member
	err            error
	bootstrapCalls int
	identity       tenancyrepo.MemberIdentity
}

func (store *membershipStub) ResolveActiveMember(_ context.Context, _ tenancy.Scope, identity tenancyrepo.MemberIdentity) (tenancyrepo.Member, error) {
	store.identity = identity
	return store.member, store.err
}

func (store *membershipStub) BootstrapDevelopmentAdministrator(context.Context, tenancy.Scope, string, string) (tenancyrepo.Member, error) {
	store.bootstrapCalls++
	if store.err != nil && !errors.Is(store.err, tenancyrepo.ErrMemberNotFound) {
		return tenancyrepo.Member{}, store.err
	}
	store.err = nil
	store.member = tenancyrepo.Member{Role: "admin", Status: "active"}
	return store.member, nil
}

func TestDatabaseMembershipOverridesOIDCRealmRole(t *testing.T) {
	scope := validTestScope(t)
	store := &membershipStub{member: tenancyrepo.Member{Role: "viewer", Status: "active"}}
	principal := Principal{Issuer: "issuer", Subject: "subject", SubjectRef: string(make([]byte, 64)), Roles: []string{"admin"}, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String()}
	resolver := claimTenantResolver{memberships: store}
	resolved, err := resolver.ResolveTenant(context.Background(), principal, httptest.NewRequest("GET", "/", nil))
	if err != nil || resolved != scope {
		t.Fatalf("ResolveTenant() = %#v, %v", resolved, err)
	}
	authorizer := roleAuthorizer{memberships: store}
	if err := authorizer.Authorize(context.Background(), principal, scope, "products.read"); err != nil {
		t.Fatalf("viewer read denied: %v", err)
	}
	if err := authorizer.Authorize(context.Background(), principal, scope, "settings.security.write"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("OIDC admin widened DB viewer role: %v", err)
	}
}

func TestDevelopmentBootstrapRequiresAdminClaim(t *testing.T) {
	scope := validTestScope(t)
	store := &membershipStub{err: tenancyrepo.ErrMemberNotFound}
	resolver := claimTenantResolver{environment: config.EnvironmentDevelopment, memberships: store}
	principal := Principal{Issuer: "issuer", Subject: "subject", SubjectRef: string(make([]byte, 64)), Roles: []string{"viewer"}, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String()}
	if _, err := resolver.ResolveTenant(context.Background(), principal, httptest.NewRequest("GET", "/", nil)); !errors.Is(err, ErrUnauthorized) || store.bootstrapCalls != 0 {
		t.Fatalf("viewer bootstrapped administrator: %v calls=%d", err, store.bootstrapCalls)
	}
	principal.Roles = []string{"admin"}
	if _, err := resolver.ResolveTenant(context.Background(), principal, httptest.NewRequest("GET", "/", nil)); err != nil || store.bootstrapCalls != 1 {
		t.Fatalf("development admin bootstrap failed: %v calls=%d", err, store.bootstrapCalls)
	}
}

func TestA02TenantResolverUsesOnlyVerifiedEmailForInvitations(t *testing.T) {
	scope := validTestScope(t)
	store := &membershipStub{member: tenancyrepo.Member{Role: "viewer", Status: "active"}}
	resolver := claimTenantResolver{memberships: store}
	principal := Principal{Issuer: "issuer", Subject: "subject", SubjectRef: "synthetic-reference", Email: "invited-admin@example.test", OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String()}
	for _, verified := range []string{"", "own-address@example.test"} {
		principal.VerifiedEmail = verified
		if _, err := resolver.ResolveTenant(t.Context(), principal, httptest.NewRequest("GET", "/", nil)); err != nil {
			t.Fatal(err)
		}
		if store.identity.SubjectRef != principal.SubjectRef || store.identity.VerifiedEmail != verified {
			t.Fatal("resolver promoted profile email to invitation ownership evidence")
		}
		if err := (roleAuthorizer{memberships: store}).Authorize(t.Context(), principal, scope, "products.read"); err != nil {
			t.Fatal(err)
		}
		if store.identity.VerifiedEmail != "" {
			t.Fatal("authorization lookup must not bind invitations")
		}
	}
}
