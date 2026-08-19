package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/config"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
)

type settingsSecurityStoreStub struct {
	sessions []securitysettings.Session
	events   []securitysettings.LoginEvent
	revoke   securitysettings.RevokeCommand
}

func (*settingsSecurityStoreStub) Observe(context.Context, tenancy.Scope, securitysettings.Observation) error {
	return nil
}
func (s *settingsSecurityStoreStub) ListSessions(context.Context, tenancy.Scope, int, string) ([]securitysettings.Session, string, error) {
	return s.sessions, "", nil
}
func (s *settingsSecurityStoreStub) ListLoginEvents(context.Context, tenancy.Scope, int, string) ([]securitysettings.LoginEvent, string, error) {
	return s.events, "", nil
}
func (s *settingsSecurityStoreStub) Revoke(_ context.Context, _ tenancy.Scope, command securitysettings.RevokeCommand) (securitysettings.Session, error) {
	s.revoke = command
	now := command.OccurredAt
	return securitysettings.Session{Ref: command.SessionRef, Status: "revoked", RevokedAt: &now}, nil
}

type settingsAuditStub struct{ records []audit.Record }

func (s settingsAuditStub) ListSettings(context.Context, tenancy.Scope, int, string) ([]audit.Record, string, error) {
	return s.records, "", nil
}

func settingsSecurityRequest(t *testing.T, method, target string, principal Principal) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	ctx := context.WithValue(request.Context(), requestScopeKey{}, validTestScope(t))
	ctx = context.WithValue(ctx, requestIdentityKey{}, principal)
	return request.WithContext(ctx)
}

func TestSettingsSecurityConfigurationDoesNotClaimProviderHealth(t *testing.T) {
	api := &settingsSecurityAPI{oidc: config.OIDC{Issuer: "https://id.example.test/realms/main", ClientID: "torgnexa-web"}}
	response := httptest.NewRecorder()
	api.configuration(response, httptest.NewRequest(http.MethodGet, SettingsSecurityConfigurationPath, nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "/realms/main") || !strings.Contains(response.Body.String(), `"provider_health":"not_verified"`) {
		t.Fatalf("configuration response = %d %s", response.Code, response.Body.String())
	}
}

func TestSettingsSecuritySessionRevocationCarriesAuditEvidence(t *testing.T) {
	ref := strings.Repeat("a", 64)
	store := &settingsSecurityStoreStub{}
	api := &settingsSecurityAPI{store: store}
	request := settingsSecurityRequest(t, http.MethodPost, SettingsSecuritySessionsPath+"/"+ref+":revoke", Principal{Issuer: "https://id.example.test", Subject: "actor", SessionRef: ref})
	request.Header.Set("Idempotency-Key", "settings-security-test")
	response := httptest.NewRecorder()
	api.revoke(response, request)
	if response.Code != http.StatusOK || store.revoke.ActorID != "actor" || store.revoke.CorrelationID != "settings-security-test" || store.revoke.SessionRef != ref || store.revoke.EventID == "" {
		t.Fatalf("revocation = %d %#v %s", response.Code, store.revoke, response.Body.String())
	}
	var body map[string]any
	if json.Unmarshal(response.Body.Bytes(), &body) != nil || body["current"] != true {
		t.Fatalf("response = %s", response.Body.String())
	}
}

func TestSettingsSecuritySessionsMinimizeSubject(t *testing.T) {
	now := time.Now().UTC()
	ref := strings.Repeat("b", 64)
	store := &settingsSecurityStoreStub{sessions: []securitysettings.Session{{Ref: ref, SubjectRef: strings.Repeat("c", 64), Status: "active", ClientKind: "browser", AuthenticatedAt: now, FirstSeenAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}}}
	api := &settingsSecurityAPI{store: store}
	response := httptest.NewRecorder()
	api.sessions(response, settingsSecurityRequest(t, http.MethodGet, SettingsSecuritySessionsPath, Principal{Issuer: "https://id.example.test", Subject: "raw-subject-must-not-leak", SessionRef: ref}))
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "raw-subject") || strings.Contains(response.Body.String(), strings.Repeat("c", 64)) || !strings.Contains(response.Body.String(), strings.Repeat("c", 12)) {
		t.Fatalf("sessions response = %d %s", response.Code, response.Body.String())
	}
}

func TestIdentityReferencesAndClientKindsAreMinimized(t *testing.T) {
	ref := identityReference("https://id.example.test", "provider-session")
	if len(ref) != 64 || strings.Contains(ref, "provider-session") {
		t.Fatalf("identity reference = %q", ref)
	}
	for input, want := range map[string]string{"Mozilla/5.0 Chrome/1": "browser", "TORGNEXA Android": "mobile", "curl/8": "api", "custom": "unknown"} {
		if got := oidcClientKind(input); got != want {
			t.Errorf("client kind %q = %q, want %q", input, got, want)
		}
	}
}
