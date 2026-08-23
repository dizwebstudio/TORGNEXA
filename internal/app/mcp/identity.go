package mcp

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/agentgovernance"
	"github.com/torgnexa/torgnexa/internal/platform/mcpaccounts"
)

// mcpAccountFinder is the one repository method this resolver needs. It is
// still a normal tenant-scoped, RLS-enforced Postgres lookup: scope is
// parsed from the presented token before this is ever called (see
// PostgresIdentityResolver.ResolveMCPIdentity), never bypassed.
type mcpAccountFinder interface {
	FindByID(ctx context.Context, scope tenancy.Scope, id string) (mcpaccounts.Account, []byte, error)
}

type mcpAccountActivityRecorder interface {
	RecordUse(context.Context, tenancy.Scope, string, time.Time) error
}

// PostgresIdentityResolver is the production IdentityResolver: it trusts
// only a bearer token whose embedded routing IDs resolve to an enabled
// mcp_client_accounts row whose stored token_hash matches the presented
// secret. This is the first working (non-deny) IdentityResolver this
// repository has had; cmd/mcp wires it in place of denyIdentityResolver.
type PostgresIdentityResolver struct{ Accounts mcpAccountFinder }

func (resolver PostgresIdentityResolver) ResolveMCPIdentity(r *http.Request) (Identity, error) {
	if resolver.Accounts == nil || r == nil {
		return Identity{}, ErrUnauthorized
	}
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return Identity{}, ErrUnauthorized
	}
	token, err := mcpaccounts.DecodeToken(strings.TrimPrefix(header, prefix))
	if err != nil {
		return Identity{}, ErrUnauthorized
	}
	scope, err := tenancy.ParseScope(token.OrganizationID, token.WorkspaceID)
	if err != nil {
		return Identity{}, ErrUnauthorized
	}
	account, tokenHash, err := resolver.Accounts.FindByID(r.Context(), scope, token.AccountID)
	if err != nil || !account.Enabled || account.RevokedAt != nil || (!account.ExpiresAt.IsZero() && !time.Now().UTC().Before(account.ExpiresAt)) {
		return Identity{}, ErrUnauthorized
	}
	if subtle.ConstantTimeCompare(mcpaccounts.HashSecret(token.Secret), tokenHash) != 1 {
		return Identity{}, ErrUnauthorized
	}
	if recorder, ok := resolver.Accounts.(mcpAccountActivityRecorder); ok {
		if err := recorder.RecordUse(r.Context(), scope, account.ID, time.Now().UTC()); err != nil {
			return Identity{}, ErrUnauthorized
		}
	}
	runID, err := sortableIDs{}.NewID()
	if err != nil {
		return Identity{}, ErrUnauthorized
	}
	identity := Identity{
		ActorID:     account.ID,
		Tenant:      scope,
		Agent:       agentgovernance.Agent{ID: account.AgentID, ModelID: account.ModelID, RunID: runID, IntegrationID: account.IntegrationID},
		Permissions: account.Permissions,
	}
	if !identity.Valid() {
		return Identity{}, ErrUnauthorized
	}
	return identity, nil
}
