package enterpriseiam

import (
	"context"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"testing"
	"time"
)

func sc(t *testing.T) tenancy.Scope {
	s, e := tenancy.ParseScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestUnmappedIdentityDeniedAndTenantMappingExplicit(t *testing.T) {
	s := sc(t)
	now := time.Now().UTC()
	r := MappingRule{ID: "m1", Protocol: ProtocolSAML, Issuer: "https://idp.example", Group: "ops", Role: "operator", OrganizationID: s.OrganizationID().String(), WorkspaceID: s.WorkspaceID().String(), Version: 1, UpdatedAt: now}
	if _, e := Evaluate(s, ExternalIdentity{Issuer: r.Issuer, Subject: "u1", Groups: []string{"sales"}}, []MappingRule{r}); e != ErrDenied {
		t.Fatalf("err=%v", e)
	}
	g, e := Evaluate(s, ExternalIdentity{Issuer: r.Issuer, Subject: "u1", Groups: []string{"ops"}}, []MappingRule{r})
	if e != nil || len(g) != 1 || g[0].Role != "operator" {
		t.Fatalf("%+v %v", g, e)
	}
}

type rev struct{ n int }

func (r *rev) RevokeSessions(context.Context, tenancy.Scope, string) error    { r.n++; return nil }
func (r *rev) RevokeAPIKeys(context.Context, tenancy.Scope, string) error     { r.n++; return nil }
func (r *rev) RevokeDelegations(context.Context, tenancy.Scope, string) error { r.n++; return nil }

type aud struct{ n int }

func (a *aud) SecurityAudit(context.Context, tenancy.Scope, string, string, time.Time) error {
	a.n++
	return nil
}
func TestOffboardingRevokesAllAccessAndAudits(t *testing.T) {
	r := &rev{}
	a := &aud{}
	if e := Offboard(context.Background(), sc(t), "subject-1", r, a, time.Now().UTC()); e != nil {
		t.Fatal(e)
	}
	if r.n != 3 || a.n != 1 {
		t.Fatalf("revocations=%d audit=%d", r.n, a.n)
	}
}
func TestPrivilegedMappingRequiresSecurityAudit(t *testing.T) {
	scope := sc(t)
	now := time.Now().UTC()
	rule := MappingRule{ID: "priv-1", Protocol: ProtocolSAML, Issuer: "https://idp.example", Group: "admins", Role: "admin", OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), Privileged: true, Version: 1, UpdatedAt: now}
	store := NewStore()
	if err := store.Put(scope, rule); err != ErrInvalid {
		t.Fatalf("unreviewed privileged mapping err=%v", err)
	}
	a := &aud{}
	if err := store.PutReviewed(context.Background(), scope, rule, a); err != nil {
		t.Fatal(err)
	}
	if a.n != 1 {
		t.Fatalf("audit=%d", a.n)
	}
}
func TestCredentialReconciliationFindsDisabledAccess(t *testing.T) {
	drift, err := ReconcileCredentialDrift([]CredentialState{{Subject: "u1", Disabled: true, ActiveSessions: 1, ActiveAPIKeys: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 2 {
		t.Fatalf("drift=%+v", drift)
	}
}
