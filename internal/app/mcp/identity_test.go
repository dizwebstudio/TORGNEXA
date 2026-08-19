package mcp

import (
	"context"
	"net/http"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/mcpaccounts"
)

const testAccountID = "018f0000-0000-7000-8000-0000000000aa"

type fakeAccountFinder struct {
	account   mcpaccounts.Account
	tokenHash []byte
	err       error
}

func (f fakeAccountFinder) FindByID(_ context.Context, scope tenancy.Scope, id string) (mcpaccounts.Account, []byte, error) {
	if f.err != nil {
		return mcpaccounts.Account{}, nil, f.err
	}
	if scope.OrganizationID().String() != testOrgID || scope.WorkspaceID().String() != testWSID || id != testAccountID {
		return mcpaccounts.Account{}, nil, mcpaccounts.ErrNotFound
	}
	return f.account, f.tokenHash, nil
}

func issuedToken(t *testing.T, enabled bool) (string, fakeAccountFinder) {
	t.Helper()
	secret, err := mcpaccounts.GenerateSecret()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	account := mcpaccounts.Account{
		ID: testAccountID, Label: "n8n", AgentID: "agent-1", ModelID: "gpt-5", IntegrationID: "n8n",
		Permissions: []string{"commerce.products.read"}, Enabled: enabled, Version: 1,
	}
	token := mcpaccounts.EncodeToken(testOrgID, testWSID, testAccountID, secret)
	return token, fakeAccountFinder{account: account, tokenHash: mcpaccounts.HashSecret(secret)}
}

func requestWithBearer(token string) *http.Request {
	r, _ := http.NewRequest(http.MethodPost, EndpointPath, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestPostgresIdentityResolverAcceptsValidToken(t *testing.T) {
	token, finder := issuedToken(t, true)
	resolver := PostgresIdentityResolver{Accounts: finder}
	identity, err := resolver.ResolveMCPIdentity(requestWithBearer(token))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity.ActorID != testAccountID || identity.Agent.ID != "agent-1" || identity.Agent.ModelID != "gpt-5" || identity.Agent.IntegrationID != "n8n" {
		t.Fatalf("unexpected identity: %+v", identity)
	}
	if identity.Agent.RunID == "" {
		t.Fatalf("expected a generated run id")
	}
	if len(identity.Permissions) != 1 || identity.Permissions[0] != "commerce.products.read" {
		t.Fatalf("unexpected permissions: %v", identity.Permissions)
	}

	second, err := resolver.ResolveMCPIdentity(requestWithBearer(token))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if second.Agent.RunID == identity.Agent.RunID {
		t.Fatalf("expected a fresh run id per request")
	}
}

func TestPostgresIdentityResolverRejectsMissingHeader(t *testing.T) {
	_, finder := issuedToken(t, true)
	resolver := PostgresIdentityResolver{Accounts: finder}
	if _, err := resolver.ResolveMCPIdentity(requestWithBearer("")); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestPostgresIdentityResolverRejectsTamperedSecret(t *testing.T) {
	token, finder := issuedToken(t, true)
	resolver := PostgresIdentityResolver{Accounts: finder}
	// Flip a character in the encoded secret segment so the presented token
	// still decodes/routes correctly but no longer hashes to the stored value.
	tampered := token[:len(token)-1] + "0"
	if tampered == token {
		tampered = token[:len(token)-1] + "1"
	}
	if _, err := resolver.ResolveMCPIdentity(requestWithBearer(tampered)); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized for a tampered token, got %v", err)
	}
}

func TestPostgresIdentityResolverRejectsDisabledAccount(t *testing.T) {
	token, finder := issuedToken(t, false)
	resolver := PostgresIdentityResolver{Accounts: finder}
	if _, err := resolver.ResolveMCPIdentity(requestWithBearer(token)); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized for a disabled account, got %v", err)
	}
}

func TestPostgresIdentityResolverRejectsWrongTenant(t *testing.T) {
	secret, err := mcpaccounts.GenerateSecret()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, finder := issuedToken(t, true)
	// A token whose embedded org/workspace do not match any account the
	// finder knows about must fail closed, not silently resolve elsewhere.
	otherOrg := "018f0000-0000-7000-8000-000000000099"
	wrongTenantToken := mcpaccounts.EncodeToken(otherOrg, testWSID, testAccountID, secret)
	resolver := PostgresIdentityResolver{Accounts: finder}
	if _, err := resolver.ResolveMCPIdentity(requestWithBearer(wrongTenantToken)); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized for a wrong-tenant token, got %v", err)
	}
}
