package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/aiadvisory"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"github.com/torgnexa/torgnexa/internal/platform/trustcontrol"
)

const (
	AIProviderAccountsPath        = "/api/v1/settings/ai-providers"
	AIProviderAccountsDisablePath = "/api/v1/settings/ai-providers:disable"
	AIProviderAnalyzePath         = "/api/v1/settings/ai-providers:analyze"
)

type aiAdvisoryRepository interface {
	List(context.Context, tenancy.Scope) ([]aiadvisory.Account, error)
	Get(context.Context, tenancy.Scope, string) (aiadvisory.Account, error)
	CreateGoverned(context.Context, tenancy.Scope, string, aiadvisory.CreateAccount, string, string, []byte, string, string) (aiadvisory.Account, bool, error)
	DisableGoverned(context.Context, tenancy.Scope, string, int64, string, []byte, string, string) (aiadvisory.Account, bool, error)
}

type aiAdvisoryAPI struct {
	repository aiAdvisoryRepository
	secrets    secrets.SecretProvider
	registry   *builtinruntime.Registry
	audit      auditCapturer
	governance aiEgressGovernance
}

type aiEgressGovernance interface {
	AuthorizeAI(context.Context, tenancy.Scope, string, string, string, string, trustcontrol.EgressRequest, []byte) (trustcontrol.Policy, error)
	RecordAI(context.Context, tenancy.Scope, string, string, string, string, string, string, trustcontrol.EgressRequest, trustcontrol.Policy, []byte) error
	ReserveExternal(context.Context, tenancy.Scope, string, string, []byte) (bool, error)
	CompleteExternal(context.Context, tenancy.Scope, string, string, string, string, string) error
}

func newAIAdvisoryRoutes(repository aiAdvisoryRepository, secretProvider secrets.SecretProvider, registry *builtinruntime.Registry, auditService auditCapturer, governance aiEgressGovernance) []ProtectedRoute {
	api := &aiAdvisoryAPI{repository: repository, secrets: secretProvider, registry: registry, audit: auditService, governance: governance}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: AIProviderAccountsPath, Permission: "settings.ai_providers.read", Handler: http.HandlerFunc(api.list)},
		{Method: http.MethodPost, Path: AIProviderAccountsPath, Permission: "settings.ai_providers.write", Handler: http.HandlerFunc(api.create)},
		{Method: http.MethodPost, Path: AIProviderAccountsDisablePath, Permission: "settings.ai_providers.write", Handler: http.HandlerFunc(api.disable)},
		{Method: http.MethodPost, Path: AIProviderAnalyzePath, Permission: "ai.analyze", Handler: http.HandlerFunc(api.analyze)},
	}
}

type aiProviderAccountView struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Label     string    `json:"label"`
	Model     string    `json:"model"`
	BaseURL   string    `json:"base_url,omitempty"`
	FolderID  string    `json:"folder_id,omitempty"`
	Enabled   bool      `json:"enabled"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func aiProviderAccountToView(account aiadvisory.Account) aiProviderAccountView {
	return aiProviderAccountView{
		ID: account.ID, Provider: string(account.Provider), Label: account.Label, Model: account.Model,
		BaseURL: account.BaseURL, FolderID: account.FolderID, Enabled: account.Enabled, Version: account.Version,
		CreatedAt: account.CreatedAt, UpdatedAt: account.UpdatedAt,
	}
}

func (api *aiAdvisoryAPI) list(w http.ResponseWriter, r *http.Request) {
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
	views := make([]aiProviderAccountView, 0, len(items))
	for _, item := range items {
		views = append(views, aiProviderAccountToView(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

type aiProviderAccountCreateRequest struct {
	Provider   string `json:"provider"`
	Label      string `json:"label"`
	Model      string `json:"model"`
	BaseURL    string `json:"base_url,omitempty"`
	FolderID   string `json:"folder_id,omitempty"`
	Credential string `json:"credential"`
}

func (api *aiAdvisoryAPI) create(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !ok || !principalOK || correlation == "" || len(correlation) > 128 || api == nil || api.repository == nil || api.secrets == nil || api.audit == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input aiProviderAccountCreateRequest
	if !decodeCatalogJSON(w, r, &input) {
		return
	}
	cmd := aiadvisory.CreateAccount{
		Provider: aiadvisory.Provider(input.Provider), Label: input.Label, Model: input.Model,
		BaseURL: input.BaseURL, FolderID: input.FolderID, Credential: []byte(input.Credential),
	}
	if err := aiadvisory.ValidateCreate(cmd); err != nil {
		zero(cmd.Credential)
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	credentialDigest := sha256.Sum256(cmd.Credential)
	_, digest, err := trustcontrol.DigestJSON(map[string]any{"provider": input.Provider, "label": input.Label, "model": input.Model, "base_url": input.BaseURL, "folder_id": input.FolderID, "credential_sha256": hex.EncodeToString(credentialDigest[:])})
	if err != nil {
		zero(cmd.Credential)
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	created, err := api.secrets.Create(r.Context(), scope, secrets.ClassAIProviderCredential, cmd.Credential)
	zero(cmd.Credential)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Credential could not be stored")
		return
	}
	id := newApprovalID()
	account, replayed, err := api.repository.CreateGoverned(r.Context(), scope, id, cmd, created.Reference.String(), correlation, digest, principal.SubjectRef, id+".e")
	if err != nil {
		if created.Reference.Valid() {
			_, _ = api.secrets.Revoke(r.Context(), scope, created.Reference)
		}
		if errors.Is(err, aiadvisory.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if replayed && created.Reference.Valid() {
		_, _ = api.secrets.Revoke(r.Context(), scope, created.Reference)
	}
	if _, auditErr := api.audit.Capture(r.Context(), scope, audit.Entry{
		ActorID: principal.Subject, Source: "api", Action: "ai_provider_account.create", ResourceType: "ai_provider_account",
		ResourceID: account.ID, CorrelationID: correlation, Risk: audit.RiskWriteSensitive,
		Summary: audit.Summary{"provider": string(account.Provider), "label": account.Label},
	}); auditErr != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Audit evidence unavailable")
		return
	}
	writeJSON(w, http.StatusCreated, aiProviderAccountToView(account))
}

type aiProviderAccountDisableRequest struct {
	AccountID       string `json:"account_id"`
	ExpectedVersion int64  `json:"expected_version"`
}

func (api *aiAdvisoryAPI) disable(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !ok || !principalOK || correlation == "" || len(correlation) > 128 || api == nil || api.repository == nil || api.audit == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input aiProviderAccountDisableRequest
	if !decodeCatalogJSON(w, r, &input) {
		return
	}
	_, digest, err := trustcontrol.DigestJSON(input)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, _, err := api.repository.DisableGoverned(r.Context(), scope, strings.TrimSpace(input.AccountID), input.ExpectedVersion, correlation, digest, principal.SubjectRef, newApprovalID())
	if err != nil {
		if errors.Is(err, aiadvisory.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if _, auditErr := api.audit.Capture(r.Context(), scope, audit.Entry{
		ActorID: principal.Subject, Source: "api", Action: "ai_provider_account.disable", ResourceType: "ai_provider_account",
		ResourceID: account.ID, CorrelationID: correlation, Risk: audit.RiskWriteSensitive,
		Summary: audit.Summary{"provider": string(account.Provider)},
	}); auditErr != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Audit evidence unavailable")
		return
	}
	writeJSON(w, http.StatusOK, aiProviderAccountToView(account))
}

type aiAnalyzeRequest struct {
	AccountID    string   `json:"account_id"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	Prompt       string   `json:"prompt"`
	DataClasses  []string `json:"data_classes"`
}
type aiAnalyzeResponse struct {
	Text     string `json:"text"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// analyze sends the caller-assembled analytics text to the tenant's chosen,
// already-configured AI provider account and returns its response. The
// request body is the only tenant text that leaves TORGNEXA; the audit trail
// intentionally records only which account/provider was used, never the
// prompt or the model's response.
func (api *aiAdvisoryAPI) analyze(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !ok || !principalOK || correlation == "" || len(correlation) > 128 || api == nil || api.repository == nil || api.secrets == nil || api.registry == nil || api.audit == nil || api.governance == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input aiAnalyzeRequest
	if !decodeCatalogJSON(w, r, &input) {
		return
	}
	if err := aiadvisory.ValidateCompletionRequest(input.SystemPrompt, input.Prompt); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.repository.Get(r.Context(), scope, strings.TrimSpace(input.AccountID))
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if !account.Enabled {
		writeProblem(w, http.StatusConflict, "AI provider account is disabled")
		return
	}
	promptBytes := len(input.SystemPrompt) + len(input.Prompt)
	egressRequest := trustcontrol.EgressRequest{Destination: string(account.Provider), Model: account.Model, DataClasses: input.DataClasses, PromptBytes: promptBytes}
	_, promptDigest, err := trustcontrol.DigestJSON(map[string]any{"system_prompt": input.SystemPrompt, "prompt": input.Prompt})
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	_, operationDigest, err := trustcontrol.DigestJSON(map[string]any{"account_id": account.ID, "provider": string(account.Provider), "model": account.Model, "data_classes": input.DataClasses, "prompt_sha256": hex.EncodeToString(promptDigest)})
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	claimed, err := api.governance.ReserveExternal(r.Context(), scope, "ai_egress.analyze", correlation, operationDigest)
	if err != nil || !claimed {
		writeProblem(w, http.StatusConflict, "Idempotent operation already consumed")
		return
	}
	policy, err := api.governance.AuthorizeAI(r.Context(), scope, newApprovalID(), principal.SubjectRef, correlation, account.ID, egressRequest, promptDigest)
	if err != nil {
		if errors.Is(err, trustcontrol.ErrDenied) {
			if completeErr := api.governance.CompleteExternal(r.Context(), scope, "ai_egress.analyze", correlation, "ai_provider_account", account.ID, "denied"); completeErr != nil {
				writeProblem(w, http.StatusServiceUnavailable, "Idempotency evidence unavailable")
				return
			}
			writeProblem(w, http.StatusForbidden, "AI egress denied")
			return
		}
		writeProblem(w, http.StatusServiceUnavailable, "AI egress evidence unavailable")
		return
	}
	redactedSystem, err := redactOptionalPrompt(input.SystemPrompt, policy.MaxPromptBytes)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	redactedPrompt, err := trustcontrol.RedactPrompt(input.Prompt, policy.MaxPromptBytes)
	if err != nil || len(redactedSystem)+len(redactedPrompt) > policy.MaxPromptBytes {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	_, promptDigest, err = trustcontrol.DigestJSON(map[string]any{"system_prompt": redactedSystem, "prompt": redactedPrompt})
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	secretReference, err := sdk.ParseSecretReference(account.SecretReference)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	sdkAccount := sdk.Account{
		ID: account.ID, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(),
		ConnectorID: string(account.Provider), Family: sdk.FamilyAI, Status: sdk.AccountActive,
		SecretReference: secretReference, Version: account.Version, Health: sdk.Health{Status: sdk.HealthUnknown},
		CreatedAt: account.CreatedAt.UTC(), UpdatedAt: account.UpdatedAt.UTC(),
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	host, err := hostFromBaseURL(account.BaseURL)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	text, model, err := api.registry.AICompletion(r.Context(), sdkAccount, runtime, host, account.FolderID, account.Model, redactedSystem, redactedPrompt)
	outcome := "succeeded"
	if err != nil {
		outcome = "failed"
	}
	if evidenceErr := api.governance.RecordAI(r.Context(), scope, newApprovalID(), principal.SubjectRef, correlation, account.ID, "outcome", outcome, egressRequest, policy, promptDigest); evidenceErr != nil {
		writeProblem(w, http.StatusServiceUnavailable, "AI egress evidence unavailable")
		return
	}
	if receiptErr := api.governance.CompleteExternal(r.Context(), scope, "ai_egress.analyze", correlation, "ai_provider_account", account.ID, outcome); receiptErr != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Idempotency evidence unavailable")
		return
	}
	if _, auditErr := api.audit.Capture(r.Context(), scope, audit.Entry{
		ActorID: principal.Subject, Source: "api", Action: "ai_provider_account.analyze", ResourceType: "ai_provider_account",
		ResourceID: account.ID, CorrelationID: correlation, Risk: audit.RiskWriteSensitive,
		Summary: audit.Summary{"provider": string(account.Provider), "ok": err == nil, "policy_version": policy.Version},
	}); auditErr != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Audit evidence unavailable")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "AI provider request failed")
		return
	}
	writeJSON(w, http.StatusOK, aiAnalyzeResponse{Text: text, Provider: string(account.Provider), Model: model})
}

func redactOptionalPrompt(value string, maxBytes int) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return trustcontrol.RedactPrompt(value, maxBytes)
}

// hostFromBaseURL extracts the bare hostname builtinruntime.AICompletion's
// host override expects. aiadvisory.ValidateCreate already guarantees a
// stored BaseURL is either empty or an "https://" URL; the connector
// transport (internal/platform/builtinruntime) rejects anything but a bare
// hostname, so the scheme/path/port must be stripped at this provider-neutral
// boundary rather than forwarded as-is.
func hostFromBaseURL(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", nil
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return "", errors.New("ai advisory: invalid base_url")
	}
	return parsed.Hostname(), nil
}

func zero(material []byte) {
	for index := range material {
		material[index] = 0
	}
}
