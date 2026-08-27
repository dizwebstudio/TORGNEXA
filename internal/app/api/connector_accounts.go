package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/connectorauth"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	runtimeconfigstore "github.com/torgnexa/torgnexa/internal/platform/postgres/connectorconfigrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

const (
	ConnectorAccountsPath        = "/api/v1/connector-accounts"
	ConnectorAccountsDisablePath = "/api/v1/connector-accounts:disable"
	ConnectorCredentialsPath     = "/api/v1/connector-accounts:credentials"
	ConnectorCapabilitiesPath    = "/api/v1/connector-accounts:capabilities"
	ConnectorEnablePath          = "/api/v1/connector-accounts:enable"
	ConnectorHealthPath          = "/api/v1/connector-accounts:check"
	ConnectorHealthHistoryPath   = "/api/v1/connector-accounts:health-history"
	ConnectorSyncPath            = "/api/v1/connector-accounts:sync"
	ConnectorOAuthStartPath      = "/api/v1/connector-accounts:oauth-start"
	ConnectorOAuthCallbackPath   = "/api/v1/connector-accounts:oauth-callback"
	ConnectorRuntimeConfigPath   = "/api/v1/connector-accounts:runtime-config"
)

type auditCapturer interface {
	Capture(context.Context, tenancy.Scope, audit.Entry) (audit.Record, error)
}

type connectorAccountAPI struct {
	repository   connectorAccountRepository
	configs      connectorRuntimeConfigRepository
	audit        auditCapturer
	secrets      secrets.SecretProvider
	sync         connectorSyncStarter
	oauthStore   connectorauth.SessionStore
	oauthRefresh connectorauth.RefreshCoordinator
	callbacks    *connectorauth.CallbackPolicy
	registry     connectorRuntimeAdmission
	exchange     connectorOAuthExchange
	now          func() time.Time
}

type connectorRuntimeConfigRepository interface {
	Config(context.Context, tenancy.Scope, string) (json.RawMessage, int64, error)
	Put(context.Context, tenancy.Scope, string, json.RawMessage, int64) (int64, error)
}

type connectorRuntimeAdmission interface {
	SupportsAccountConfiguration(string) bool
	SupportsCapability(string, string) bool
	SupportsSync(string, string, string) bool
	RuntimeConfigRequired(string) bool
	Health(context.Context, sdk.Account, sdk.Runtime, func(context.Context, string) (json.RawMessage, error)) (sdk.Health, error)
}

type connectorAccountRepository interface {
	sdk.AccountRepository
	ListAccounts(context.Context, string, string, string, int) ([]sdk.Account, error)
	BindSecret(context.Context, string, string, string, sdk.SecretReference, int64) (sdk.Account, error)
	AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error)
	ReplaceAccountCapabilities(context.Context, tenancy.Scope, string, int64, sdk.Manifest, []sdk.Capability) (sdk.Account, []sdk.AccountCapabilitySetting, error)
	HealthHistory(context.Context, tenancy.Scope, string, int) ([]connectorrepo.HealthSnapshot, error)
}

type connectorOAuthExchange func(context.Context, sdk.OAuth2Configuration, connectorauth.OAuthClient, string, string, string, time.Duration) ([]byte, error)

type connectorSyncStarter interface {
	Start(context.Context, tenancy.Scope, string, string, time.Time) (int, error)
}

type connectorAccountCreateRequest struct {
	AccountID       string `json:"account_id"`
	ConnectorID     string `json:"connector_id"`
	SecretReference string `json:"secret_reference,omitempty"`
}

type connectorAccountDisableRequest struct {
	AccountID       string `json:"account_id"`
	ExpectedVersion int64  `json:"expected_version"`
}
type connectorAccountActionRequest struct {
	AccountID       string `json:"account_id"`
	ExpectedVersion int64  `json:"expected_version"`
}

type connectorCredentialRequest struct {
	AccountID       string `json:"account_id"`
	ExpectedVersion int64  `json:"expected_version"`
	MaterialBase64  string `json:"material_base64"`
}

type connectorOAuthStartRequest struct {
	AccountID       string `json:"account_id"`
	ExpectedVersion int64  `json:"expected_version"`
	CallbackURL     string `json:"callback_url"`
}

type connectorOAuthCallbackRequest struct {
	Code        string `json:"code"`
	State       string `json:"state"`
	CallbackURL string `json:"callback_url"`
}

type connectorOAuthStartResponse struct {
	AuthorizationURL string `json:"authorization_url"`
	ExpiresAt        string `json:"expires_at"`
}

type connectorRuntimeConfigRequest struct {
	AccountID       string          `json:"account_id"`
	ExpectedVersion int64           `json:"expected_version"`
	Config          json.RawMessage `json:"config"`
}

type connectorRuntimeConfigView struct {
	AccountID string          `json:"account_id"`
	Version   int64           `json:"version"`
	Config    json.RawMessage `json:"config"`
}

type connectorCapabilitiesRequest struct {
	AccountID       string           `json:"account_id"`
	ExpectedVersion int64            `json:"expected_version"`
	Enabled         []sdk.Capability `json:"enabled"`
}

type connectorAccountView struct {
	ID               string                         `json:"id"`
	ConnectorID      string                         `json:"connector_id"`
	Family           sdk.Family                     `json:"family"`
	Status           string                         `json:"status"`
	SecretReference  string                         `json:"secret_reference,omitempty"`
	Version          int64                          `json:"version"`
	HealthStatus     string                         `json:"health_status"`
	HealthReasonCode string                         `json:"health_reason_code,omitempty"`
	HealthCheckedAt  string                         `json:"health_checked_at,omitempty"`
	CreatedAt        string                         `json:"created_at"`
	UpdatedAt        string                         `json:"updated_at"`
	Capabilities     []sdk.AccountCapabilitySetting `json:"capabilities"`
}

type connectorAccountPage struct {
	Items      []connectorAccountView `json:"items"`
	NextCursor string                 `json:"next_cursor,omitempty"`
}

func newConnectorAccountRoutes(repository *connectorrepo.Repository, configs *runtimeconfigstore.Repository, auditService auditCapturer, secretProvider secrets.SecretProvider, refreshCoordinator connectorauth.RefreshCoordinator, callbackPolicy *connectorauth.CallbackPolicy, registry connectorRuntimeAdmission, syncStarters ...connectorSyncStarter) []ProtectedRoute {
	var syncStarter connectorSyncStarter
	if len(syncStarters) > 0 {
		syncStarter = syncStarters[0]
	}
	api := &connectorAccountAPI{repository: repository, configs: configs, audit: auditService, secrets: secretProvider, sync: syncStarter, oauthStore: repository, oauthRefresh: refreshCoordinator, callbacks: callbackPolicy, registry: registry, exchange: connectorauth.HTTPExchange, now: func() time.Time { return time.Now().UTC() }}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: ConnectorAccountsPath, Permission: "connectors.accounts.read", Handler: http.HandlerFunc(api.list)},
		{Method: http.MethodPost, Path: ConnectorAccountsPath, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.create)},
		{Method: http.MethodPost, Path: ConnectorAccountsDisablePath, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.disable)},
		{Method: http.MethodPost, Path: ConnectorCredentialsPath, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.credentials)},
		{Method: http.MethodPut, Path: ConnectorCapabilitiesPath, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.capabilities)},
		{Method: http.MethodPost, Path: ConnectorEnablePath, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.enable)},
		{Method: http.MethodPost, Path: ConnectorHealthPath, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.check)},
		{Method: http.MethodGet, Path: ConnectorHealthHistoryPath, Permission: "connectors.accounts.read", Handler: http.HandlerFunc(api.healthHistory)},
		{Method: http.MethodPost, Path: ConnectorSyncPath, Permission: "sync.write", Handler: http.HandlerFunc(api.syncNow)},
		{Method: http.MethodPost, Path: ConnectorOAuthStartPath, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.oauthStart)},
		{Method: http.MethodPost, Path: ConnectorOAuthCallbackPath, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.oauthCallback)},
		{Method: http.MethodGet, Path: ConnectorRuntimeConfigPath, Permission: "connectors.accounts.read", Handler: http.HandlerFunc(api.runtimeConfigGet)},
		{Method: http.MethodPut, Path: ConnectorRuntimeConfigPath, Permission: "connectors.accounts.write", Handler: http.HandlerFunc(api.runtimeConfigPut)},
	}
}

func (api *connectorAccountAPI) runtimeConfigGet(w http.ResponseWriter, request *http.Request) {
	scope, ok := ScopeFromContext(request.Context())
	if !ok || api == nil || api.repository == nil || api.configs == nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	accountID := strings.TrimSpace(request.URL.Query().Get("account_id"))
	if accountID == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	_, err := api.repository.AccountByID(request.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	raw, version, err := api.configs.Config(request.Context(), scope, accountID)
	if errors.Is(err, runtimeconfigstore.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, connectorRuntimeConfigView{AccountID: accountID, Version: version, Config: raw})
}

func (api *connectorAccountAPI) runtimeConfigPut(w http.ResponseWriter, request *http.Request) {
	scope, ok := ScopeFromContext(request.Context())
	if !ok || api == nil || api.repository == nil || api.configs == nil || api.audit == nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	var input connectorRuntimeConfigRequest
	if err := decodeStrictJSON(request, &input); err != nil || input.ExpectedVersion < 0 || strings.TrimSpace(request.Header.Get("Idempotency-Key")) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.repository.AccountByID(request.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), input.AccountID)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	version, err := api.configs.Put(request.Context(), scope, account.ID, input.Config, input.ExpectedVersion)
	switch {
	case errors.Is(err, runtimeconfigstore.ErrInvalid):
		writeProblem(w, http.StatusUnprocessableEntity, "Runtime configuration must be non-secret JSON")
		return
	case errors.Is(err, runtimeconfigstore.ErrConflict):
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	case err != nil:
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	principal, ok := PrincipalFromContext(request.Context())
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	_, err = api.audit.Capture(request.Context(), scope, audit.Entry{ActorID: principal.Subject, Source: "api", Action: "connector.account.runtime_config_updated", ResourceType: "connector_account", ResourceID: account.ID, CorrelationID: request.Header.Get("Idempotency-Key"), Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"connector_id": account.ConnectorID, "config_version": version}})
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, connectorRuntimeConfigView{AccountID: account.ID, Version: version, Config: input.Config})
}

func (api *connectorAccountAPI) credentials(w http.ResponseWriter, request *http.Request) {
	scope, ok := ScopeFromContext(request.Context())
	if !ok || api == nil || api.repository == nil || api.secrets == nil || api.audit == nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	var input connectorCredentialRequest
	if err := decodeStrictJSON(request, &input); err != nil || strings.TrimSpace(request.Header.Get("Idempotency-Key")) == "" || len(input.MaterialBase64) > 90_000 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	material, err := base64.StdEncoding.DecodeString(input.MaterialBase64)
	if err != nil || len(material) == 0 || len(material) > 64<<10 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	defer func() { clear(material) }()
	account, err := api.repository.AccountByID(request.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), input.AccountID)
	if err != nil || account.Version != input.ExpectedVersion {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	manifest, err := sdk.CatalogManifest(account.ConnectorID)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	class, err := credentialClass(manifest)
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "OAuth configuration required")
		return
	}
	if class == secrets.ClassOAuthClient {
		if _, err = connectorauth.ParseOAuthClient(material); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
	}
	metadata, err := api.secrets.Create(request.Context(), scope, class, material)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	bound, err := api.repository.BindSecret(request.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), account.ID, sdk.SecretReference(metadata.Reference.String()), account.Version)
	if err != nil {
		_, _ = api.secrets.Revoke(request.Context(), scope, metadata.Reference)
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if account.SecretReference != "" {
		if old, parseErr := secrets.ParseReference(string(account.SecretReference)); parseErr == nil {
			_, _ = api.secrets.Revoke(request.Context(), scope, old)
		}
	}
	if err := api.capture(request, scope, "connector.account.credentials_enrolled", bound, audit.RiskWriteSensitive); err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, accountView(bound))
}

func credentialClass(manifest sdk.Manifest) (secrets.Class, error) {
	class := secrets.ClassConnectorToken
	for _, requirement := range manifest.Auth {
		switch requirement.Kind {
		case sdk.AuthOAuth2:
			class = secrets.ClassOAuthClient
		case sdk.AuthCertificate:
			class = secrets.ClassCertificate
		case sdk.AuthBasic:
			class = secrets.ClassERPCredential
		case sdk.AuthAPIKey, sdk.AuthBearer, sdk.AuthNone:
		default:
			return "", sdk.ErrInvalidManifest
		}
	}
	return class, nil
}

func (api *connectorAccountAPI) list(w http.ResponseWriter, request *http.Request) {
	scope, ok := ScopeFromContext(request.Context())
	if !ok || api == nil || api.repository == nil || api.audit == nil || api.registry == nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	limit := 50
	if raw := request.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		limit = value
	}
	afterID, err := decodeAccountCursor(request.URL.Query().Get("cursor"))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	accounts, err := api.repository.ListAccounts(request.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), afterID, limit+1)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	page := connectorAccountPage{Items: make([]connectorAccountView, 0, min(limit, len(accounts)))}
	if len(accounts) > limit {
		page.NextCursor = encodeAccountCursor(accounts[limit-1].ID)
		accounts = accounts[:limit]
	}
	for _, account := range accounts {
		settings, settingsErr := api.repository.AccountCapabilities(request.Context(), scope, account.ID)
		if settingsErr != nil {
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		if len(settings) == 0 {
			manifest, manifestErr := sdk.CatalogManifest(account.ConnectorID)
			if manifestErr != nil {
				writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
				return
			}
			settings, settingsErr = sdk.BuildAccountCapabilitySettings(manifest, nil)
			if settingsErr != nil {
				writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
				return
			}
		}
		page.Items = append(page.Items, accountViewWithCapabilities(account, settings))
	}
	writeJSON(w, http.StatusOK, page)
}

func (api *connectorAccountAPI) capabilities(w http.ResponseWriter, request *http.Request) {
	scope, ok := ScopeFromContext(request.Context())
	if !ok || api == nil || api.repository == nil || api.audit == nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	var input connectorCapabilitiesRequest
	if decodeStrictJSON(request, &input) != nil || strings.TrimSpace(request.Header.Get("Idempotency-Key")) == "" || input.Enabled == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.repository.AccountByID(request.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), input.AccountID)
	if err != nil || account.Version != input.ExpectedVersion {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	manifest, err := sdk.CatalogManifest(account.ConnectorID)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	for _, capability := range input.Enabled {
		executable := api.registry.SupportsCapability(account.ConnectorID, string(capability))
		if !executable {
			writeProblem(w, http.StatusUnprocessableEntity, "Capability is not executable by this runtime")
			return
		}
	}
	updated, settings, err := api.repository.ReplaceAccountCapabilities(request.Context(), scope, account.ID, input.ExpectedVersion, manifest, input.Enabled)
	if errors.Is(err, sdk.ErrInvalidCapabilitySettings) {
		writeProblem(w, http.StatusUnprocessableEntity, "Capability is not declared by connector manifest")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if err = api.captureCapabilities(request, scope, updated, settings); err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, accountViewWithCapabilities(updated, settings))
}

func (api *connectorAccountAPI) create(w http.ResponseWriter, request *http.Request) {
	scope, ok := ScopeFromContext(request.Context())
	if !ok || api == nil || api.repository == nil || api.registry == nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	var input connectorAccountCreateRequest
	if err := decodeStrictJSON(request, &input); err != nil || request.Header.Get("Idempotency-Key") != input.AccountID {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	manifest, err := sdk.CatalogManifest(input.ConnectorID)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	available := api.registry.SupportsAccountConfiguration(input.ConnectorID)
	if !available {
		writeProblem(w, http.StatusUnprocessableEntity, "Connector runtime unavailable")
		return
	}
	secretReference, err := sdk.ParseSecretReference(input.SecretReference)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	command := sdk.AccountCreate{ID: input.AccountID, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), ConnectorID: input.ConnectorID, SecretReference: secretReference}
	account, err := api.repository.CreateAccount(request.Context(), command, manifest)
	if err != nil {
		existing, lookupErr := api.repository.AccountByID(request.Context(), command.OrganizationID, command.WorkspaceID, command.ID)
		if lookupErr == nil && accountCreateFingerprint(existing) == commandCreateFingerprint(command) {
			writeJSON(w, http.StatusOK, accountView(existing))
			return
		}
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if err := api.capture(request, scope, "connector.account.created", account, audit.RiskWriteSafe); err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusCreated, accountView(account))
}

func accountCreateFingerprint(account sdk.Account) string {
	return strings.Join([]string{account.ID, account.OrganizationID, account.WorkspaceID, account.ConnectorID, string(account.SecretReference)}, "\x00")
}

func commandCreateFingerprint(command sdk.AccountCreate) string {
	return strings.Join([]string{command.ID, command.OrganizationID, command.WorkspaceID, command.ConnectorID, string(command.SecretReference)}, "\x00")
}

func (api *connectorAccountAPI) disable(w http.ResponseWriter, request *http.Request) {
	scope, ok := ScopeFromContext(request.Context())
	if !ok || api == nil || api.repository == nil || api.audit == nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	var input connectorAccountDisableRequest
	if err := decodeStrictJSON(request, &input); err != nil || strings.TrimSpace(request.Header.Get("Idempotency-Key")) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.repository.ChangeAccountStatus(request.Context(), sdk.AccountStatusChange{OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), AccountID: input.AccountID, Status: sdk.AccountDisabled, ExpectedVersion: input.ExpectedVersion})
	if err != nil {
		if errors.Is(err, sdk.ErrAccountConflict) {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if err := api.capture(request, scope, "connector.account.disabled", account, audit.RiskWriteSensitive); err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, accountView(account))
}

func (api *connectorAccountAPI) healthHistory(w http.ResponseWriter, request *http.Request) {
	scope, ok := ScopeFromContext(request.Context())
	if !ok || api == nil || api.repository == nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	accountID := strings.TrimSpace(request.URL.Query().Get("account_id"))
	limit := 20
	maxHealthHistory := connectorrepo.MaxHealthHistory
	if raw := request.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > maxHealthHistory {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		limit = v
	}
	if accountID == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	items, err := api.repository.HealthHistory(request.Context(), scope, accountID, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Items []connectorrepo.HealthSnapshot `json:"items"`
	}{Items: items})
}

func (api *connectorAccountAPI) check(w http.ResponseWriter, request *http.Request) {
	scope, ok := ScopeFromContext(request.Context())
	if !ok || api == nil || api.repository == nil || api.secrets == nil || api.audit == nil || api.registry == nil {
		writeProblem(w, 500, "Internal Server Error")
		return
	}
	var input connectorAccountActionRequest
	if decodeStrictJSON(request, &input) != nil || strings.TrimSpace(request.Header.Get("Idempotency-Key")) == "" {
		writeProblem(w, 400, "Bad Request")
		return
	}
	account, err := api.repository.AccountByID(request.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), input.AccountID)
	if err != nil || account.Version != input.ExpectedVersion {
		writeProblem(w, 409, "Conflict")
		return
	}
	available := api.registry.SupportsAccountConfiguration(account.ConnectorID)
	if !available {
		writeProblem(w, http.StatusUnprocessableEntity, "Connector runtime unavailable")
		return
	}
	manifest, err := sdk.CatalogManifest(account.ConnectorID)
	if err != nil {
		writeProblem(w, 400, "Bad Request")
		return
	}
	health := sdk.Health{}
	if manifest.RequiresSecret() && account.SecretReference == "" {
		health = sdk.Health{Status: sdk.HealthUnavailable, ReasonCode: "credentials_missing", CheckedAt: api.now().UTC()}
	}
	if health.Status == "" {
		runtime, runtimeErr := connectorruntime.NewForAccount(api.secrets, api.oauthRefresh, scope, account)
		if runtimeErr != nil {
			health = sdk.Health{Status: sdk.HealthUnavailable, ReasonCode: "runtime_unavailable", CheckedAt: api.now().UTC()}
		} else {
			switch oauthPreparation(runtime.PrepareOAuth(request.Context())) {
			case "reauthorization_required":
				health = sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "oauth_reauthorization_required", CheckedAt: api.now().UTC()}
			case "refresh_failed":
				health = sdk.Health{Status: sdk.HealthUnavailable, ReasonCode: "oauth_refresh_failed", CheckedAt: api.now().UTC()}
			default:
				var loader func(context.Context, string) (json.RawMessage, error)
				if api.configs != nil {
					loader = func(ctx context.Context, accountID string) (json.RawMessage, error) {
						raw, _, loadErr := api.configs.Config(ctx, scope, accountID)
						return raw, loadErr
					}
				}
				health, err = api.registry.Health(request.Context(), account, runtime, loader)
				if err != nil {
					health = sdk.Health{Status: sdk.HealthUnavailable, ReasonCode: "runtime_probe_failed", CheckedAt: api.now().UTC()}
				}
			}
		}
	}
	updated, err := api.repository.RecordAccountHealth(request.Context(), sdk.AccountHealthUpdate{OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), AccountID: account.ID, Health: health, ExpectedVersion: account.Version})
	if err != nil {
		writeProblem(w, 409, "Conflict")
		return
	}
	if err = api.capture(request, scope, "connector.account.connection_checked", updated, audit.RiskWriteSafe); err != nil {
		writeProblem(w, 500, "Internal Server Error")
		return
	}
	writeJSON(w, 200, accountView(updated))
}

func oauthPreparation(err error) string {
	switch {
	case err == nil:
		return "ready"
	case errors.Is(err, connectorauth.ErrOAuthReauthorizationRequired):
		return "reauthorization_required"
	default:
		return "refresh_failed"
	}
}

func (api *connectorAccountAPI) oauthStart(w http.ResponseWriter, request *http.Request) {
	scope, scopeOK := ScopeFromContext(request.Context())
	principal, principalOK := PrincipalFromContext(request.Context())
	key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	var input connectorOAuthStartRequest
	if !scopeOK || !principalOK || api == nil || api.repository == nil || api.oauthStore == nil || api.secrets == nil || api.callbacks == nil || key == "" || len(key) > 128 || decodeStrictJSON(request, &input) != nil || api.callbacks.Validate(input.CallbackURL) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.repository.AccountByID(request.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), input.AccountID)
	if err != nil || account.Version != input.ExpectedVersion || account.Status != sdk.AccountDisabled || account.SecretReference == "" {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	manifest, err := sdk.CatalogManifest(account.ConnectorID)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	configuration, err := connectorauth.OAuthConfiguration(manifest)
	if err != nil || configuration.GrantType != "authorization_code" {
		writeProblem(w, http.StatusUnprocessableEntity, "Browser OAuth is not supported")
		return
	}
	client, err := api.readOAuthClient(request.Context(), scope, account.SecretReference)
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "OAuth client configuration required")
		return
	}
	pending, challenge, err := connectorauth.NewPKCE()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	payload, _ := json.Marshal(pending)
	metadata, err := api.secrets.Create(request.Context(), scope, secrets.ClassOAuthState, payload)
	clear(payload)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	digest, _ := connectorauth.StateDigest(pending.State)
	now := api.now().UTC()
	proposed := connectorauth.Session{ID: newApprovalID(), AccountID: account.ID, AccountVersion: account.Version, ActorID: principal.Subject, StateDigest: digest, PendingSecretRef: metadata.Reference.String(), CallbackURL: input.CallbackURL, CorrelationID: key, Status: "pending", CreatedAt: now, ExpiresAt: now.Add(connectorauth.OAuthSessionTTL)}
	stored, replayed, err := api.oauthStore.CreateOrReplay(request.Context(), scope, proposed)
	if err != nil {
		_, _ = api.secrets.Revoke(request.Context(), scope, metadata.Reference)
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if replayed {
		_, _ = api.secrets.Revoke(request.Context(), scope, metadata.Reference)
		pending, err = api.readPending(request.Context(), scope, stored.PendingSecretRef)
		if err != nil {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		digestBytes := sha256.Sum256([]byte(pending.CodeVerifier))
		challenge = base64.RawURLEncoding.EncodeToString(digestBytes[:])
	}
	authorizationURL, err := connectorauth.AuthorizationURL(configuration, client.ClientID, stored.CallbackURL, pending.State, challenge)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if err = api.capture(request, scope, "connector.account.oauth_started", account, audit.RiskWriteSensitive); err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, connectorOAuthStartResponse{AuthorizationURL: authorizationURL, ExpiresAt: stored.ExpiresAt.UTC().Format(time.RFC3339)})
}

func (api *connectorAccountAPI) oauthCallback(w http.ResponseWriter, request *http.Request) {
	scope, scopeOK := ScopeFromContext(request.Context())
	principal, principalOK := PrincipalFromContext(request.Context())
	key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	var input connectorOAuthCallbackRequest
	if !scopeOK || !principalOK || api == nil || api.repository == nil || api.oauthStore == nil || api.secrets == nil || api.callbacks == nil || key == "" || decodeStrictJSON(request, &input) != nil || api.callbacks.Validate(input.CallbackURL) != nil || len(input.Code) > 8192 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	digest, err := connectorauth.StateDigest(input.State)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	session, err := api.oauthStore.Consume(request.Context(), scope, digest, principal.Subject, input.CallbackURL, api.now().UTC())
	if err != nil {
		writeProblem(w, http.StatusConflict, "OAuth state is invalid or already used")
		return
	}
	pendingReference, referenceErr := secrets.ParseReference(session.PendingSecretRef)
	if referenceErr != nil {
		writeProblem(w, http.StatusConflict, "OAuth state is invalid or already used")
		return
	}
	defer func() { _, _ = api.secrets.Revoke(request.Context(), scope, pendingReference) }()
	pending, err := api.readPending(request.Context(), scope, session.PendingSecretRef)
	if err != nil || subtle.ConstantTimeCompare([]byte(pending.State), []byte(input.State)) != 1 {
		writeProblem(w, http.StatusConflict, "OAuth state is invalid or already used")
		return
	}
	account, err := api.repository.AccountByID(request.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), session.AccountID)
	if err != nil || account.Version != session.AccountVersion || account.SecretReference == "" {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	manifest, err := sdk.CatalogManifest(account.ConnectorID)
	configuration, configurationErr := connectorauth.OAuthConfiguration(manifest)
	client, clientErr := api.readOAuthClient(request.Context(), scope, account.SecretReference)
	if err != nil || configurationErr != nil || clientErr != nil || configuration.GrantType != "authorization_code" {
		writeProblem(w, http.StatusUnprocessableEntity, "OAuth configuration is invalid")
		return
	}
	bundle, err := api.oauthExchange(request.Context(), configuration, client, input.Code, input.CallbackURL, pending.CodeVerifier, min(time.Duration(manifest.RateLimit.RequestTimeoutMS)*time.Millisecond, 15*time.Second))
	if err != nil {
		_, _ = api.repository.RecordAccountHealth(request.Context(), sdk.AccountHealthUpdate{OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), AccountID: account.ID, ExpectedVersion: account.Version, Health: sdk.Health{Status: sdk.HealthUnavailable, ReasonCode: "oauth_exchange_failed", CheckedAt: api.now().UTC()}})
		writeProblem(w, http.StatusBadGateway, "OAuth provider rejected the callback")
		return
	}
	metadata, err := api.secrets.Create(request.Context(), scope, secrets.ClassOAuthRefresh, bundle)
	clear(bundle)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	bound, err := api.repository.BindSecret(request.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), account.ID, sdk.SecretReference(metadata.Reference.String()), account.Version)
	if err != nil {
		_, _ = api.secrets.Revoke(request.Context(), scope, metadata.Reference)
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	oldReference, _ := secrets.ParseReference(string(account.SecretReference))
	_, _ = api.secrets.Revoke(request.Context(), scope, oldReference)
	if err = api.capture(request, scope, "connector.account.oauth_completed", bound, audit.RiskWriteSensitive); err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, accountView(bound))
}

func (api *connectorAccountAPI) oauthExchange(ctx context.Context, configuration sdk.OAuth2Configuration, client connectorauth.OAuthClient, code, callbackURL, verifier string, timeout time.Duration) ([]byte, error) {
	if api.exchange == nil {
		return nil, connectorauth.ErrRemoteUnavailable
	}
	return api.exchange(ctx, configuration, client, code, callbackURL, verifier, timeout)
}

func (api *connectorAccountAPI) readOAuthClient(ctx context.Context, scope tenancy.Scope, reference sdk.SecretReference) (connectorauth.OAuthClient, error) {
	parsed, err := secrets.ParseReference(string(reference))
	if err != nil {
		return connectorauth.OAuthClient{}, err
	}
	var client connectorauth.OAuthClient
	var parseErr error
	err = api.secrets.Use(ctx, scope, parsed, func(material []byte) error { client, parseErr = connectorauth.ParseOAuthClient(material); return nil })
	if err != nil || parseErr != nil {
		return connectorauth.OAuthClient{}, connectorauth.ErrInvalid
	}
	return client, nil
}

func (api *connectorAccountAPI) readPending(ctx context.Context, scope tenancy.Scope, reference string) (connectorauth.PendingMaterial, error) {
	parsed, err := secrets.ParseReference(reference)
	if err != nil {
		return connectorauth.PendingMaterial{}, err
	}
	var pending connectorauth.PendingMaterial
	err = api.secrets.Use(ctx, scope, parsed, func(material []byte) error { return json.Unmarshal(material, &pending) })
	if err != nil || pending.State == "" || pending.CodeVerifier == "" {
		return connectorauth.PendingMaterial{}, connectorauth.ErrInvalid
	}
	return pending, nil
}

func (api *connectorAccountAPI) enable(w http.ResponseWriter, request *http.Request) {
	scope, ok := ScopeFromContext(request.Context())
	if !ok || api == nil || api.repository == nil || api.audit == nil || api.registry == nil {
		writeProblem(w, 500, "Internal Server Error")
		return
	}
	var input connectorAccountActionRequest
	if decodeStrictJSON(request, &input) != nil || strings.TrimSpace(request.Header.Get("Idempotency-Key")) == "" {
		writeProblem(w, 400, "Bad Request")
		return
	}
	current, err := api.repository.AccountByID(request.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), input.AccountID)
	if err != nil || current.Version != input.ExpectedVersion {
		writeProblem(w, 409, "Conflict")
		return
	}
	available := api.registry.SupportsAccountConfiguration(current.ConnectorID)
	if !available {
		writeProblem(w, http.StatusUnprocessableEntity, "Connector runtime unavailable")
		return
	}
	if current.Health.Status != sdk.HealthHealthy {
		writeProblem(w, 422, "Connection check required")
		return
	}
	settings, err := api.repository.AccountCapabilities(request.Context(), scope, current.ID)
	if err != nil {
		writeProblem(w, 500, "Internal Server Error")
		return
	}
	for _, setting := range settings {
		executable := api.registry.SupportsCapability(current.ConnectorID, string(setting.Capability))
		if setting.Enabled && !executable {
			writeProblem(w, http.StatusUnprocessableEntity, "Enabled capability is not executable by this runtime")
			return
		}
	}
	required := api.registry.RuntimeConfigRequired(current.ConnectorID)
	if required {
		if api.configs == nil {
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		if _, _, configErr := api.configs.Config(request.Context(), scope, current.ID); configErr != nil {
			writeProblem(w, http.StatusUnprocessableEntity, "Runtime configuration required")
			return
		}
	}
	if !hasEnabledCapability(settings) {
		writeProblem(w, 422, "At least one account capability must be enabled")
		return
	}
	updated, err := api.repository.ChangeAccountStatus(request.Context(), sdk.AccountStatusChange{OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), AccountID: current.ID, Status: sdk.AccountActive, ExpectedVersion: current.Version})
	if err != nil {
		writeProblem(w, 409, "Conflict")
		return
	}
	if err = api.capture(request, scope, "connector.account.enabled", updated, audit.RiskWriteSensitive); err != nil {
		writeProblem(w, 500, "Internal Server Error")
		return
	}
	writeJSON(w, 200, accountView(updated))
}

func (api *connectorAccountAPI) syncNow(w http.ResponseWriter, request *http.Request) {
	scope, ok := ScopeFromContext(request.Context())
	principal, pok := PrincipalFromContext(request.Context())
	if !ok || !pok || api == nil || api.repository == nil || api.sync == nil || api.registry == nil {
		writeProblem(w, 500, "Internal Server Error")
		return
	}
	var input connectorAccountActionRequest
	if decodeStrictJSON(request, &input) != nil || strings.TrimSpace(request.Header.Get("Idempotency-Key")) == "" {
		writeProblem(w, 400, "Bad Request")
		return
	}
	account, err := api.repository.AccountByID(request.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), input.AccountID)
	if err != nil || account.Version != input.ExpectedVersion {
		writeProblem(w, 409, "Conflict")
		return
	}
	available := api.registry.SupportsAccountConfiguration(account.ConnectorID)
	if !available {
		writeProblem(w, http.StatusUnprocessableEntity, "Connector runtime unavailable")
		return
	}
	if account.Status != sdk.AccountActive || account.Health.Status != sdk.HealthHealthy {
		writeProblem(w, 422, "Active healthy account required")
		return
	}
	count, err := api.sync.Start(request.Context(), scope, account.ID, principal.Subject, time.Now().UTC())
	if errors.Is(err, syncengine.ErrPreviewUnavailable) {
		writeProblem(w, http.StatusUnprocessableEntity, "Current bootstrap preview required before remote write")
		return
	}
	if err != nil {
		writeProblem(w, 409, "Sync policy required")
		return
	}
	writeJSON(w, 202, map[string]any{"started": count, "account_id": account.ID})
}

func (api *connectorAccountAPI) capture(request *http.Request, scope tenancy.Scope, action string, account sdk.Account, risk audit.Risk) error {
	principal, ok := PrincipalFromContext(request.Context())
	if !ok {
		return ErrUnauthenticated
	}
	_, err := api.audit.Capture(request.Context(), scope, audit.Entry{ActorID: principal.Subject, Source: "api", Action: action, ResourceType: "connector_account", ResourceID: account.ID, CorrelationID: request.Header.Get("Idempotency-Key"), Risk: risk, Summary: audit.Summary{"connector_id": account.ConnectorID, "status": string(account.Status), "version": account.Version, "has_secret_reference": account.SecretReference != ""}})
	return err
}

func (api *connectorAccountAPI) captureCapabilities(request *http.Request, scope tenancy.Scope, account sdk.Account, settings []sdk.AccountCapabilitySetting) error {
	principal, ok := PrincipalFromContext(request.Context())
	if !ok {
		return ErrUnauthenticated
	}
	enabled, writes := 0, 0
	for _, setting := range settings {
		if setting.Enabled {
			enabled++
			if setting.Direction == sdk.CapabilityWrite {
				writes++
			}
		}
	}
	_, err := api.audit.Capture(request.Context(), scope, audit.Entry{
		ActorID: principal.Subject, Source: "api", Action: "connector.account.capabilities_changed",
		ResourceType: "connector_account", ResourceID: account.ID, CorrelationID: request.Header.Get("Idempotency-Key"),
		Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"connector_id": account.ConnectorID, "version": account.Version, "enabled_count": enabled, "write_count": writes},
	})
	return err
}

func decodeStrictJSON(request *http.Request, output any) error {
	if request.Body == nil || !strings.HasPrefix(request.Header.Get("Content-Type"), "application/json") {
		return errors.New("invalid JSON request")
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func accountView(account sdk.Account) connectorAccountView {
	return accountViewWithCapabilities(account, []sdk.AccountCapabilitySetting{})
}

func accountViewWithCapabilities(account sdk.Account, settings []sdk.AccountCapabilitySetting) connectorAccountView {
	checkedAt := ""
	if !account.Health.CheckedAt.IsZero() {
		checkedAt = account.Health.CheckedAt.UTC().Format(time.RFC3339)
	}
	return connectorAccountView{ID: account.ID, ConnectorID: account.ConnectorID, Family: account.Family, Status: string(account.Status), SecretReference: string(account.SecretReference), Version: account.Version, HealthStatus: string(account.Health.Status), HealthReasonCode: account.Health.ReasonCode, HealthCheckedAt: checkedAt, CreatedAt: account.CreatedAt.Format(time.RFC3339), UpdatedAt: account.UpdatedAt.Format(time.RFC3339), Capabilities: settings}
}

func hasEnabledCapability(settings []sdk.AccountCapabilitySetting) bool {
	for _, setting := range settings {
		if setting.Enabled {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func encodeAccountCursor(id string) string {
	return "v1." + base64.RawURLEncoding.EncodeToString([]byte(id))
}
func decodeAccountCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	if !strings.HasPrefix(cursor, "v1.") || len(cursor) > 256 {
		return "", errors.New("invalid cursor")
	}
	value, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, "v1."))
	if err != nil || len(value) == 0 || len(value) > 128 || strings.ContainsAny(string(value), "\r\n\t ") {
		return "", errors.New("invalid cursor")
	}
	return string(value), nil
}
