package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
)

const (
	IdentityProviderSettingsPath   = "/api/v1/settings/identity-providers"
	identityProviderSettingsPrefix = "/api/v1/settings/identity-providers/"
)

type identityProviderSettingsAPI struct {
	store     securitysettings.IdentityProviderStore
	secrets   secrets.SecretProvider
	audit     auditCapturer
	policy    *securitysettings.ProviderURLPolicy
	validator securitysettings.ProviderValidator
}

type identityProviderDraftRequest struct {
	Protocol        string `json:"protocol"`
	DisplayName     string `json:"display_name"`
	IssuerURL       string `json:"issuer_url"`
	ClientID        string `json:"client_id"`
	CallbackURL     string `json:"callback_url"`
	ClientSecret    string `json:"client_secret,omitempty"`
	ExpectedVersion uint64 `json:"expected_version"`
}

type identityProviderActionRequest struct {
	ExpectedVersion uint64 `json:"expected_version"`
	TargetRevision  uint64 `json:"target_revision,omitempty"`
}

type identityProviderView struct {
	ProviderID       string     `json:"provider_id"`
	Protocol         string     `json:"protocol"`
	DisplayName      string     `json:"display_name"`
	IssuerURL        string     `json:"issuer_url"`
	ClientID         string     `json:"client_id"`
	CallbackURL      string     `json:"callback_url"`
	SecretConfigured bool       `json:"secret_configured"`
	Revision         uint64     `json:"revision"`
	Version          uint64     `json:"version"`
	ActiveRevision   uint64     `json:"active_revision,omitempty"`
	Enabled          bool       `json:"enabled"`
	ValidationStatus string     `json:"validation_status"`
	ValidationReason string     `json:"validation_reason"`
	ValidatedAt      *time.Time `json:"validated_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func newIdentityProviderSettingsRoutes(store securitysettings.IdentityProviderStore, secretProvider secrets.SecretProvider, auditService auditCapturer, policy *securitysettings.ProviderURLPolicy, validator securitysettings.ProviderValidator) []ProtectedRoute {
	api := &identityProviderSettingsAPI{store: store, secrets: secretProvider, audit: auditService, policy: policy, validator: validator}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: IdentityProviderSettingsPath, Permission: "settings.identity_providers.read", Handler: http.HandlerFunc(api.list)},
		{Method: http.MethodPut, Path: identityProviderSettingsPrefix, PathPrefix: true, Permission: "settings.identity_providers.write", Handler: http.HandlerFunc(api.save)},
		{Method: http.MethodPost, Path: identityProviderSettingsPrefix, PathPrefix: true, Permission: "settings.identity_providers.write", Handler: http.HandlerFunc(api.action)},
	}
}

func (api *identityProviderSettingsAPI) list(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api == nil || api.store == nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	items, err := api.store.ListProviders(r.Context(), scope, 100)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	views := make([]identityProviderView, 0, len(items))
	for _, item := range items {
		views = append(views, identityProviderToView(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (api *identityProviderSettingsAPI) save(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	idpID, action, pathOK := identityProviderPath(r.URL.Path)
	correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !ok || !pathOK || action != "" || correlation == "" || len(correlation) > 128 || api == nil || api.store == nil || api.policy == nil || api.audit == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input identityProviderDraftRequest
	if decodeStrictJSON(r, &input) != nil || len(input.ClientSecret) > 65536 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	now := time.Now().UTC()
	draft := securitysettings.ProviderDraft{ID: idpID, Protocol: input.Protocol, DisplayName: input.DisplayName, IssuerURL: input.IssuerURL, ClientID: input.ClientID, CallbackURL: input.CallbackURL, ExpectedVersion: input.ExpectedVersion, CorrelationID: correlation, CreatedAt: now}
	if err := api.policy.ValidateDraft(r.Context(), draft); err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "Provider URLs are not allowed")
		return
	}
	if existing, err := api.store.Provider(r.Context(), scope, idpID); err == nil && existing.LastCorrelationID == correlation {
		writeJSON(w, http.StatusOK, identityProviderToView(existing))
		return
	} else if err != nil && !errors.Is(err, securitysettings.ErrIdentityNotFound) {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	var created secrets.Metadata
	if input.ClientSecret != "" {
		if api.secrets == nil {
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		material := []byte(input.ClientSecret)
		input.ClientSecret = ""
		var err error
		created, err = api.secrets.Create(r.Context(), scope, secrets.ClassOAuthClient, material)
		for index := range material {
			material[index] = 0
		}
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "Client secret could not be stored")
			return
		}
		draft.SecretReference = created.Reference.String()
	}
	item, err := api.store.SaveProvider(r.Context(), scope, draft)
	if (err != nil || item.Replayed) && created.Reference.Valid() {
		_, _ = api.secrets.Revoke(r.Context(), scope, created.Reference)
	}
	if errors.Is(err, securitysettings.ErrIdentityConflict) {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if !item.Replayed {
		err = api.capture(r, scope, "settings.identity_provider.draft_saved", item, correlation)
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, identityProviderToView(item))
}

func (api *identityProviderSettingsAPI) action(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	idpID, action, pathOK := identityProviderPath(r.URL.Path)
	correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !ok || !pathOK || action == "" || correlation == "" || len(correlation) > 128 || api == nil || api.store == nil || api.audit == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input identityProviderActionRequest
	if decodeStrictJSON(r, &input) != nil || input.ExpectedVersion == 0 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	now := time.Now().UTC()
	var item securitysettings.ProviderConfiguration
	var err error
	switch action {
	case "validate":
		if api.validator == nil {
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		item, err = api.store.Provider(r.Context(), scope, idpID)
		if err == nil && item.LastCorrelationID == correlation {
			writeJSON(w, http.StatusOK, identityProviderToView(item))
			return
		}
		if err == nil && item.Version != input.ExpectedVersion {
			err = securitysettings.ErrIdentityConflict
		}
		if err == nil {
			var evidence securitysettings.ProviderValidation
			evidence, err = api.validator.Validate(r.Context(), item.ProviderRevision)
			evidence.ID, evidence.IdentityID, evidence.Revision, evidence.CheckedAt, evidence.ExpectedVersion, evidence.CorrelationID = newApprovalID(), idpID, item.Revision, now, input.ExpectedVersion, correlation
			if err != nil {
				evidence.Status, evidence.ReasonCode, evidence.MetadataDigest = "invalid", "discovery_failed", ""
				evidence.Issuer, evidence.AuthorizationURL, evidence.TokenURL, evidence.JWKSURL = "", "", "", ""
				if errors.Is(err, securitysettings.ErrIdentityUnsafeURL) {
					evidence.ReasonCode = "unsafe_metadata_url"
				}
			}
			item, err = api.store.RecordProviderValidation(r.Context(), scope, evidence)
			if err == nil && item.Replayed {
				writeJSON(w, http.StatusOK, identityProviderToView(item))
				return
			}
			if err == nil && evidence.Status == "invalid" {
				if captureErr := api.capture(r, scope, "settings.identity_provider.validation_failed", item, correlation); captureErr != nil {
					writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
					return
				}
				writeProblem(w, http.StatusUnprocessableEntity, "Provider validation failed")
				return
			}
		}
	case "activate":
		item, err = api.store.ActivateProvider(r.Context(), scope, idpID, input.ExpectedVersion, correlation, now)
	case "rollback":
		item, err = api.store.RollbackProvider(r.Context(), scope, idpID, input.TargetRevision, input.ExpectedVersion, newApprovalID(), correlation, now)
	case "disable":
		item, err = api.store.DisableProvider(r.Context(), scope, idpID, input.ExpectedVersion, correlation, now)
	default:
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if errors.Is(err, securitysettings.ErrIdentityNotFound) {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if errors.Is(err, securitysettings.ErrIdentityConflict) {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if errors.Is(err, securitysettings.ErrIdentityNotValidated) {
		writeProblem(w, http.StatusUnprocessableEntity, "Validated revision required")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if !item.Replayed {
		err = api.capture(r, scope, "settings.identity_provider."+action, item, correlation)
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, identityProviderToView(item))
}

func (api *identityProviderSettingsAPI) capture(r *http.Request, scope tenancy.Scope, action string, item securitysettings.ProviderConfiguration, correlation string) error {
	principal, _ := PrincipalFromContext(r.Context())
	_, err := api.audit.Capture(r.Context(), scope, audit.Entry{ActorID: principal.Subject, Source: "api", Action: action, ResourceType: "identity_provider", ResourceID: item.ID, CorrelationID: correlation, Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"protocol": item.Protocol, "revision": item.Revision, "active_revision": item.ActiveRevision, "enabled": item.Enabled, "validation_status": item.ValidationStatus}})
	return err
}

func identityProviderPath(path string) (string, string, bool) {
	tail := strings.TrimPrefix(path, identityProviderSettingsPrefix)
	if tail == path || tail == "" || strings.Contains(tail, "/") {
		return "", "", false
	}
	idpID, action, _ := strings.Cut(tail, ":")
	if idpID == "" || len(idpID) > 64 || idpID != strings.ToLower(idpID) {
		return "", "", false
	}
	return idpID, action, true
}

func identityProviderToView(item securitysettings.ProviderConfiguration) identityProviderView {
	return identityProviderView{ProviderID: item.ID, Protocol: item.Protocol, DisplayName: item.DisplayName, IssuerURL: item.IssuerURL, ClientID: item.ClientID, CallbackURL: item.CallbackURL, SecretConfigured: item.SecretReference != "", Revision: item.Revision, Version: item.Version, ActiveRevision: item.ActiveRevision, Enabled: item.Enabled, ValidationStatus: item.ValidationStatus, ValidationReason: item.ValidationReason, ValidatedAt: item.ValidatedAt, UpdatedAt: item.UpdatedAt}
}
