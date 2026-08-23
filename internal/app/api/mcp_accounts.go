package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/mcpaccounts"
	"github.com/torgnexa/torgnexa/internal/platform/trustcontrol"
)

const (
	MCPAccountsPath        = "/api/v1/settings/mcp-accounts"
	MCPAccountsDisablePath = "/api/v1/settings/mcp-accounts:disable"
	MCPAccountsRotatePath  = "/api/v1/settings/mcp-accounts:rotate"
)

type mcpAccountsRepository interface {
	List(context.Context, tenancy.Scope) ([]mcpaccounts.Account, error)
	CreateGoverned(context.Context, tenancy.Scope, string, mcpaccounts.CreateAccount, []byte, time.Time, string, []byte, string, string) (mcpaccounts.Account, bool, error)
	RotateGoverned(context.Context, tenancy.Scope, string, string, int64, []byte, time.Time, string, []byte, string, string) (mcpaccounts.Account, bool, error)
	RevokeGoverned(context.Context, tenancy.Scope, string, int64, string, []byte, string, string) (mcpaccounts.Account, bool, error)
}

type mcpAccountsAPI struct {
	repository mcpAccountsRepository
	audit      auditCapturer
}

func newMCPAccountRoutes(repository mcpAccountsRepository, auditService auditCapturer) []ProtectedRoute {
	api := &mcpAccountsAPI{repository: repository, audit: auditService}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: MCPAccountsPath, Permission: "settings.mcp_accounts.read", Handler: http.HandlerFunc(api.list)},
		{Method: http.MethodPost, Path: MCPAccountsPath, Permission: "settings.mcp_accounts.write", Handler: http.HandlerFunc(api.create)},
		{Method: http.MethodPost, Path: MCPAccountsDisablePath, Permission: "settings.mcp_accounts.write", Handler: http.HandlerFunc(api.disable)},
		{Method: http.MethodPost, Path: MCPAccountsRotatePath, Permission: "settings.mcp_accounts.write", Handler: http.HandlerFunc(api.rotate)},
	}
}

type mcpAccountView struct {
	ID               string     `json:"id"`
	Label            string     `json:"label"`
	AgentID          string     `json:"agent_id"`
	ModelID          string     `json:"model_id"`
	IntegrationID    string     `json:"integration_id"`
	Permissions      []string   `json:"permissions"`
	Enabled          bool       `json:"enabled"`
	Version          int64      `json:"version"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	RotatedFromID    string     `json:"rotated_from_id,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty"`
	UseCount         int64      `json:"use_count"`
	CredentialStatus string     `json:"credential_status"`
}

func mcpAccountToView(account mcpaccounts.Account) mcpAccountView {
	return mcpAccountView{
		ID: account.ID, Label: account.Label, AgentID: account.AgentID, ModelID: account.ModelID, IntegrationID: account.IntegrationID,
		Permissions: account.Permissions, Enabled: account.Enabled, Version: account.Version,
		CreatedAt: account.CreatedAt, UpdatedAt: account.UpdatedAt, ExpiresAt: account.ExpiresAt, RotatedFromID: account.RotatedFromID,
		RevokedAt: account.RevokedAt, LastUsedAt: account.LastUsedAt, UseCount: account.UseCount, CredentialStatus: mcpCredentialStatus(account, time.Now().UTC()),
	}
}

func mcpCredentialStatus(account mcpaccounts.Account, now time.Time) string {
	if account.RevokedAt != nil || !account.Enabled {
		return "revoked"
	}
	if !account.ExpiresAt.IsZero() && !now.Before(account.ExpiresAt) {
		return "expired"
	}
	return "active"
}

func (api *mcpAccountsAPI) list(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api == nil || api.repository == nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	items, err := api.repository.List(r.Context(), scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	views := make([]mcpAccountView, 0, len(items))
	for _, item := range items {
		views = append(views, mcpAccountToView(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

type mcpAccountCreateRequest struct {
	Label         string   `json:"label"`
	AgentID       string   `json:"agent_id"`
	ModelID       string   `json:"model_id"`
	IntegrationID string   `json:"integration_id"`
	Permissions   []string `json:"permissions"`
	ExpiresInDays int      `json:"expires_in_days,omitempty"`
}

type mcpAccountCreateResponse struct {
	Account mcpAccountView `json:"account"`
	// Token is the caller's bearer credential for POST /mcp. It is
	// generated here, never stored, and returned exactly once: only its
	// SHA-256 hash is persisted (mcpaccounts.HashSecret), so this response
	// is the only time it will ever be visible again.
	Token    string `json:"token,omitempty"`
	Replayed bool   `json:"replayed"`
}

func (api *mcpAccountsAPI) create(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !ok || !principalOK || correlation == "" || len(correlation) > 128 || api == nil || api.repository == nil || api.audit == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input mcpAccountCreateRequest
	if !decodeCatalogJSON(w, r, &input) {
		return
	}
	cmd := mcpaccounts.CreateAccount{
		Label: input.Label, AgentID: input.AgentID, ModelID: input.ModelID, IntegrationID: input.IntegrationID,
		Permissions: input.Permissions,
	}
	if err := mcpaccounts.ValidateCreate(cmd); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	expiresInDays := input.ExpiresInDays
	if expiresInDays == 0 {
		expiresInDays = 90
	}
	if expiresInDays < 1 || expiresInDays > 365 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	_, digest, err := trustcontrol.DigestJSON(input)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	secret, err := mcpaccounts.GenerateSecret()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	id := newApprovalID()
	tokenHash := mcpaccounts.HashSecret(secret)
	account, replayed, err := api.repository.CreateGoverned(r.Context(), scope, id, cmd, tokenHash, time.Now().UTC().Add(time.Duration(expiresInDays)*24*time.Hour), correlation, digest, principal.SubjectRef, id+".e")
	if err != nil {
		zero(secret)
		if errors.Is(err, mcpaccounts.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	token := ""
	if !replayed {
		token = mcpaccounts.EncodeToken(scope.OrganizationID().String(), scope.WorkspaceID().String(), account.ID, secret)
	}
	zero(secret)
	if _, auditErr := api.audit.Capture(r.Context(), scope, audit.Entry{
		ActorID: principal.Subject, Source: "api", Action: "mcp_client_account.create", ResourceType: "mcp_client_account",
		ResourceID: account.ID, CorrelationID: correlation, Risk: audit.RiskWriteSensitive,
		Summary: audit.Summary{"label": account.Label, "permissions": account.Permissions},
	}); auditErr != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Audit evidence unavailable")
		return
	}
	writeJSON(w, http.StatusCreated, mcpAccountCreateResponse{Account: mcpAccountToView(account), Token: token, Replayed: replayed})
}

type mcpAccountDisableRequest struct {
	AccountID       string `json:"account_id"`
	ExpectedVersion int64  `json:"expected_version"`
}

func (api *mcpAccountsAPI) disable(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !ok || !principalOK || correlation == "" || len(correlation) > 128 || api == nil || api.repository == nil || api.audit == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input mcpAccountDisableRequest
	if !decodeCatalogJSON(w, r, &input) {
		return
	}
	_, digest, err := trustcontrol.DigestJSON(input)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, _, err := api.repository.RevokeGoverned(r.Context(), scope, strings.TrimSpace(input.AccountID), input.ExpectedVersion, correlation, digest, principal.SubjectRef, newApprovalID())
	if err != nil {
		if errors.Is(err, mcpaccounts.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if _, auditErr := api.audit.Capture(r.Context(), scope, audit.Entry{
		ActorID: principal.Subject, Source: "api", Action: "mcp_client_account.disable", ResourceType: "mcp_client_account",
		ResourceID: account.ID, CorrelationID: correlation, Risk: audit.RiskWriteSensitive,
		Summary: audit.Summary{"label": account.Label},
	}); auditErr != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Audit evidence unavailable")
		return
	}
	writeJSON(w, http.StatusOK, mcpAccountToView(account))
}

type mcpAccountRotateRequest struct {
	AccountID       string `json:"account_id"`
	ExpectedVersion int64  `json:"expected_version"`
	ExpiresInDays   int    `json:"expires_in_days,omitempty"`
}

func (api *mcpAccountsAPI) rotate(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scopeOK || !principalOK || correlation == "" || len(correlation) > 128 || api == nil || api.repository == nil || api.audit == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input mcpAccountRotateRequest
	if !decodeCatalogJSON(w, r, &input) {
		return
	}
	days := input.ExpiresInDays
	if days == 0 {
		days = 90
	}
	if days < 1 || days > 365 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	_, digest, err := trustcontrol.DigestJSON(input)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	secret, err := mcpaccounts.GenerateSecret()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	defer zero(secret)
	newID := newApprovalID()
	account, replayed, err := api.repository.RotateGoverned(r.Context(), scope, strings.TrimSpace(input.AccountID), newID, input.ExpectedVersion, mcpaccounts.HashSecret(secret), time.Now().UTC().Add(time.Duration(days)*24*time.Hour), correlation, digest, principal.SubjectRef, newID+".e")
	if err != nil {
		if errors.Is(err, mcpaccounts.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	token := ""
	if !replayed {
		token = mcpaccounts.EncodeToken(scope.OrganizationID().String(), scope.WorkspaceID().String(), account.ID, secret)
	}
	if _, auditErr := api.audit.Capture(r.Context(), scope, audit.Entry{ActorID: principal.Subject, Source: "api", Action: "mcp_client_account.rotate", ResourceType: "mcp_client_account", ResourceID: account.ID, CorrelationID: correlation, Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"rotated_from_id": account.RotatedFromID, "expires_at": account.ExpiresAt}}); auditErr != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Audit evidence unavailable")
		return
	}
	writeJSON(w, http.StatusCreated, mcpAccountCreateResponse{Account: mcpAccountToView(account), Token: token, Replayed: replayed})
}
