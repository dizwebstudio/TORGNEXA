package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/config"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
)

const (
	SettingsSecurityConfigurationPath = "/api/v1/settings/security/configuration"
	SettingsSecuritySessionsPath      = "/api/v1/settings/security/sessions"
	SettingsSecurityLoginsPath        = "/api/v1/settings/security/logins"
	SettingsSecurityAuditPath         = "/api/v1/settings/security/audit"
)

type settingsSecurityAPI struct {
	store securitysettings.Store
	audit securitysettings.SettingsAuditReader
	oidc  config.OIDC
}

func newSettingsSecurityRoutes(store securitysettings.Store, reader securitysettings.SettingsAuditReader, oidc config.OIDC) []ProtectedRoute {
	api := &settingsSecurityAPI{store: store, audit: reader, oidc: oidc}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: SettingsSecurityConfigurationPath, Permission: "settings.security.read", Handler: http.HandlerFunc(api.configuration)},
		{Method: http.MethodGet, Path: SettingsSecuritySessionsPath, Permission: "settings.security.read", Handler: http.HandlerFunc(api.sessions)},
		{Method: http.MethodPost, Path: SettingsSecuritySessionsPath + "/", PathPrefix: true, Permission: "settings.security.write", Handler: http.HandlerFunc(api.revoke)},
		{Method: http.MethodGet, Path: SettingsSecurityLoginsPath, Permission: "settings.security.read", Handler: http.HandlerFunc(api.logins)},
		{Method: http.MethodGet, Path: SettingsSecurityAuditPath, Permission: "settings.security.read", Handler: http.HandlerFunc(api.settingsAudit)},
	}
}

func (api *settingsSecurityAPI) configuration(w http.ResponseWriter, r *http.Request) {
	if api == nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	issuer, err := url.Parse(api.oidc.Issuer)
	if err != nil || issuer.Host == "" {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"provider": "oidc", "issuer": api.oidc.Issuer,
		"client_id": api.oidc.ClientID, "configuration_status": "configured",
		"configuration_source": "runtime", "provider_health": "not_verified",
	})
}

func securityPage(r *http.Request) (int, string, bool) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			return 0, "", false
		}
		limit = value
	}
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if len(cursor) > 64 || cursor != r.URL.Query().Get("cursor") {
		return 0, "", false
	}
	return limit, cursor, true
}

func (api *settingsSecurityAPI) sessions(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	limit, cursor, pageOK := securityPage(r)
	if !scopeOK || !principalOK || !pageOK || api == nil || api.store == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	items, next, err := api.store.ListSessions(r.Context(), scope, limit, cursor)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	views := make([]map[string]any, 0, len(items))
	for _, item := range items {
		userRef := item.SubjectRef
		if len(userRef) > 12 {
			userRef = userRef[:12]
		}
		views = append(views, map[string]any{"session_ref": item.Ref, "user_ref": userRef, "status": item.Status, "client_kind": item.ClientKind, "authenticated_at": item.AuthenticatedAt, "first_seen_at": item.FirstSeenAt, "last_seen_at": item.LastSeenAt, "expires_at": item.ExpiresAt, "revoked_at": item.RevokedAt, "current": item.Ref == principal.SessionRef})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views, "next_cursor": next})
}

func (api *settingsSecurityAPI) logins(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	limit, cursor, pageOK := securityPage(r)
	if !ok || !pageOK || api == nil || api.store == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	items, next, err := api.store.ListLoginEvents(r.Context(), scope, limit, cursor)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next, "evidence_scope": "torgnexa_observed_oidc_sessions"})
}

func (api *settingsSecurityAPI) revoke(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	tail := strings.TrimPrefix(r.URL.Path, SettingsSecuritySessionsPath+"/")
	ref, found := strings.CutSuffix(tail, ":revoke")
	if !scopeOK || !principalOK || !found || len(ref) != 64 || strings.Contains(ref, "/") || correlation == "" || len(correlation) > 128 || api == nil || api.store == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	item, err := api.store.Revoke(r.Context(), scope, securitysettings.RevokeCommand{EventID: newApprovalID(), SessionRef: ref, ActorID: principal.Subject, CorrelationID: correlation, OccurredAt: time.Now().UTC()})
	if errors.Is(err, securitysettings.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session_ref": item.Ref, "status": item.Status, "revoked_at": item.RevokedAt, "current": item.Ref == principal.SessionRef})
}

func (api *settingsSecurityAPI) settingsAudit(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	limit, cursor, pageOK := securityPage(r)
	if !ok || !pageOK || api == nil || api.audit == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	items, next, err := api.audit.ListSettings(r.Context(), scope, limit, cursor)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	views := make([]auditItem, 0, len(items))
	for _, item := range items {
		views = append(views, auditItem{ID: item.ID, ActorID: item.ActorID, Source: item.Source, Action: item.Action, ResourceType: item.ResourceType, ResourceID: item.ResourceID, CorrelationID: item.CorrelationID, Risk: item.Risk, Summary: item.Summary, CreatedAt: item.CreatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views, "next_cursor": next})
}
