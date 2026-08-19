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
)

const (
	MCPAccountsPath        = "/api/v1/settings/mcp-accounts"
	MCPAccountsDisablePath = "/api/v1/settings/mcp-accounts:disable"
)

type mcpAccountsRepository interface {
	List(context.Context, tenancy.Scope) ([]mcpaccounts.Account, error)
	Create(context.Context, tenancy.Scope, string, mcpaccounts.CreateAccount, []byte) (mcpaccounts.Account, error)
	Disable(context.Context, tenancy.Scope, string, int64) (mcpaccounts.Account, error)
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
	}
}

type mcpAccountView struct {
	ID            string    `json:"id"`
	Label         string    `json:"label"`
	AgentID       string    `json:"agent_id"`
	ModelID       string    `json:"model_id"`
	IntegrationID string    `json:"integration_id"`
	Permissions   []string  `json:"permissions"`
	Enabled       bool      `json:"enabled"`
	Version       int64     `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func mcpAccountToView(account mcpaccounts.Account) mcpAccountView {
	return mcpAccountView{
		ID: account.ID, Label: account.Label, AgentID: account.AgentID, ModelID: account.ModelID, IntegrationID: account.IntegrationID,
		Permissions: account.Permissions, Enabled: account.Enabled, Version: account.Version,
		CreatedAt: account.CreatedAt, UpdatedAt: account.UpdatedAt,
	}
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
}

type mcpAccountCreateResponse struct {
	Account mcpAccountView `json:"account"`
	// Token is the caller's bearer credential for POST /mcp. It is
	// generated here, never stored, and returned exactly once: only its
	// SHA-256 hash is persisted (mcpaccounts.HashSecret), so this response
	// is the only time it will ever be visible again.
	Token string `json:"token"`
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
	secret, err := mcpaccounts.GenerateSecret()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	id := newApprovalID()
	tokenHash := mcpaccounts.HashSecret(secret)
	account, err := api.repository.Create(r.Context(), scope, id, cmd, tokenHash)
	if err != nil {
		zero(secret)
		if errors.Is(err, mcpaccounts.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	token := mcpaccounts.EncodeToken(scope.OrganizationID().String(), scope.WorkspaceID().String(), account.ID, secret)
	zero(secret)
	_, _ = api.audit.Capture(r.Context(), scope, audit.Entry{
		ActorID: principal.Subject, Source: "api", Action: "mcp_client_account.create", ResourceType: "mcp_client_account",
		ResourceID: account.ID, CorrelationID: correlation, Risk: audit.RiskWriteSensitive,
		Summary: audit.Summary{"label": account.Label, "permissions": account.Permissions},
	})
	writeJSON(w, http.StatusCreated, mcpAccountCreateResponse{Account: mcpAccountToView(account), Token: token})
}

type mcpAccountDisableRequest struct {
	AccountID       string `json:"account_id"`
	ExpectedVersion int64  `json:"expected_version"`
}

func (api *mcpAccountsAPI) disable(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	if !ok || !principalOK || api == nil || api.repository == nil || api.audit == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input mcpAccountDisableRequest
	if !decodeCatalogJSON(w, r, &input) {
		return
	}
	account, err := api.repository.Disable(r.Context(), scope, strings.TrimSpace(input.AccountID), input.ExpectedVersion)
	if err != nil {
		if errors.Is(err, mcpaccounts.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	_, _ = api.audit.Capture(r.Context(), scope, audit.Entry{
		ActorID: principal.Subject, Source: "api", Action: "mcp_client_account.disable", ResourceType: "mcp_client_account",
		ResourceID: account.ID, CorrelationID: newApprovalID(), Risk: audit.RiskWriteSensitive,
		Summary: audit.Summary{"label": account.Label},
	})
	writeJSON(w, http.StatusOK, mcpAccountToView(account))
}
