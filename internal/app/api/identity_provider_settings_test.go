package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
)

type idpResolver map[string][]net.IP

func (r idpResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	items := make([]net.IPAddr, 0, len(r[host]))
	for _, ip := range r[host] {
		items = append(items, net.IPAddr{IP: ip})
	}
	return items, nil
}

type idpStoreStub struct {
	item       securitysettings.ProviderConfiguration
	validation securitysettings.ProviderValidation
}

func (s *idpStoreStub) ListProviders(context.Context, tenancy.Scope, int) ([]securitysettings.ProviderConfiguration, error) {
	if s.item.ID == "" {
		return nil, nil
	}
	return []securitysettings.ProviderConfiguration{s.item}, nil
}
func (s *idpStoreStub) Provider(context.Context, tenancy.Scope, string) (securitysettings.ProviderConfiguration, error) {
	return s.item, nil
}
func (s *idpStoreStub) SaveProvider(_ context.Context, _ tenancy.Scope, value securitysettings.ProviderDraft) (securitysettings.ProviderConfiguration, error) {
	s.item = securitysettings.ProviderConfiguration{ProviderRevision: securitysettings.ProviderRevision{ID: value.ID, Protocol: value.Protocol, DisplayName: value.DisplayName, IssuerURL: value.IssuerURL, ClientID: value.ClientID, CallbackURL: value.CallbackURL, SecretReference: value.SecretReference, Revision: 1, CreatedAt: value.CreatedAt}, Version: 1, ValidationStatus: "not_validated", ValidationReason: "not_validated", UpdatedAt: value.CreatedAt}
	return s.item, nil
}
func (s *idpStoreStub) RecordProviderValidation(_ context.Context, _ tenancy.Scope, value securitysettings.ProviderValidation) (securitysettings.ProviderConfiguration, error) {
	s.validation = value
	s.item.ValidationStatus, s.item.ValidationReason = value.Status, value.ReasonCode
	return s.item, nil
}
func (s *idpStoreStub) ActivateProvider(context.Context, tenancy.Scope, string, uint64, string, time.Time) (securitysettings.ProviderConfiguration, error) {
	s.item.Enabled, s.item.ActiveRevision, s.item.Version = true, s.item.Revision, s.item.Version+1
	return s.item, nil
}
func (s *idpStoreStub) RollbackProvider(context.Context, tenancy.Scope, string, uint64, uint64, string, string, time.Time) (securitysettings.ProviderConfiguration, error) {
	return s.item, nil
}
func (s *idpStoreStub) DisableProvider(context.Context, tenancy.Scope, string, uint64, string, time.Time) (securitysettings.ProviderConfiguration, error) {
	s.item.Enabled, s.item.Version = false, s.item.Version+1
	return s.item, nil
}

type idpSecretsStub struct{ material string }

func (s *idpSecretsStub) Create(_ context.Context, scope tenancy.Scope, class secrets.Class, material []byte) (secrets.Metadata, error) {
	s.material = string(material)
	return secrets.Metadata{Reference: secrets.Reference("sec:v1:0123456789abcdef0123456789abcdef"), OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Class: class, Status: secrets.StatusActive, CurrentVersion: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, nil
}
func (*idpSecretsStub) Use(context.Context, tenancy.Scope, secrets.Reference, func([]byte) error) error {
	return nil
}
func (*idpSecretsStub) Describe(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	return secrets.Metadata{}, nil
}
func (*idpSecretsStub) Rotate(context.Context, tenancy.Scope, secrets.Reference, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, nil
}
func (*idpSecretsStub) Revoke(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	return secrets.Metadata{}, nil
}

type idpAuditStub struct{ actions []string }

func (s *idpAuditStub) Capture(_ context.Context, _ tenancy.Scope, entry audit.Entry) (audit.Record, error) {
	s.actions = append(s.actions, entry.Action)
	return audit.Record{}, nil
}

type idpValidatorStub struct{ err error }

func (s idpValidatorStub) Validate(context.Context, securitysettings.ProviderRevision) (securitysettings.ProviderValidation, error) {
	return securitysettings.ProviderValidation{Status: "valid", ReasonCode: "validated", MetadataDigest: strings.Repeat("a", 64), Issuer: "https://id.example.test", AuthorizationURL: "https://id.example.test/auth", TokenURL: "https://id.example.test/token", JWKSURL: "https://id.example.test/jwks"}, s.err
}

func TestIdentityProviderDraftStoresSecretWithoutReturningIt(t *testing.T) {
	policy, _ := securitysettings.NewProviderURLPolicy([]string{"id.example.test"}, []string{"https://console.example.test"}, idpResolver{"id.example.test": {net.ParseIP("8.8.8.8")}})
	store, secretStore, auditor := &idpStoreStub{}, &idpSecretsStub{}, &idpAuditStub{}
	api := identityProviderSettingsAPI{store: store, secrets: secretStore, audit: auditor, policy: policy}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/settings/identity-providers/corporate", strings.NewReader(`{"protocol":"oidc","display_name":"Corporate ID","issuer_url":"https://id.example.test","client_id":"client","callback_url":"https://console.example.test/oidc/callback","client_secret":"private-client-secret","expected_version":0}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "save-corporate-1")
	response := httptest.NewRecorder()
	api.save(response, bootstrapRequestContext(t, request))
	if response.Code != http.StatusOK || secretStore.material != "private-client-secret" || strings.Contains(response.Body.String(), "private-client-secret") || !strings.Contains(response.Body.String(), `"secret_configured":true`) || len(auditor.actions) != 1 {
		t.Fatalf("status=%d secret=%q audit=%v body=%s", response.Code, secretStore.material, auditor.actions, response.Body.String())
	}
}

func TestIdentityProviderValidationFailureIsPersistedAndBlocksActivationEvidence(t *testing.T) {
	now := time.Now().UTC()
	store := &idpStoreStub{item: securitysettings.ProviderConfiguration{ProviderRevision: securitysettings.ProviderRevision{ID: "corporate", Protocol: "oidc", IssuerURL: "https://id.example.test", Revision: 1, CreatedAt: now}, Version: 1, ValidationStatus: "not_validated", UpdatedAt: now}}
	api := identityProviderSettingsAPI{store: store, audit: &idpAuditStub{}, validator: idpValidatorStub{err: securitysettings.ErrIdentityUnsafeURL}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/settings/identity-providers/corporate:validate", strings.NewReader(`{"expected_version":1}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "validate-corporate-1")
	response := httptest.NewRecorder()
	api.action(response, bootstrapRequestContext(t, request))
	if response.Code != http.StatusUnprocessableEntity || store.validation.Status != "invalid" || store.validation.ReasonCode != "unsafe_metadata_url" || store.item.Enabled {
		t.Fatalf("status=%d validation=%+v item=%+v body=%s", response.Code, store.validation, store.item, response.Body.String())
	}
}
