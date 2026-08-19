package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/connectorauth"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

type oauthAccountRepositoryStub struct{ account sdk.Account }

func (stub *oauthAccountRepositoryStub) CreateAccount(context.Context, sdk.AccountCreate, sdk.Manifest) (sdk.Account, error) {
	panic("unexpected CreateAccount")
}
func (stub *oauthAccountRepositoryStub) ListAccounts(context.Context, string, string, string, int) ([]sdk.Account, error) {
	panic("unexpected ListAccounts")
}
func (stub *oauthAccountRepositoryStub) AccountByID(context.Context, string, string, string) (sdk.Account, error) {
	return stub.account, nil
}
func (stub *oauthAccountRepositoryStub) ChangeAccountStatus(context.Context, sdk.AccountStatusChange) (sdk.Account, error) {
	panic("unexpected ChangeAccountStatus")
}
func (stub *oauthAccountRepositoryStub) RecordAccountHealth(context.Context, sdk.AccountHealthUpdate) (sdk.Account, error) {
	panic("unexpected RecordAccountHealth")
}
func (stub *oauthAccountRepositoryStub) BindSecret(_ context.Context, _, _, _ string, reference sdk.SecretReference, expectedVersion int64) (sdk.Account, error) {
	if stub.account.Version != expectedVersion {
		return sdk.Account{}, sdk.ErrAccountConflict
	}
	stub.account.SecretReference = reference
	stub.account.Status = sdk.AccountDisabled
	stub.account.Health = sdk.Health{Status: sdk.HealthUnknown}
	stub.account.Version++
	stub.account.UpdatedAt = stub.account.UpdatedAt.Add(time.Second)
	return stub.account, nil
}
func (stub *oauthAccountRepositoryStub) AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error) {
	panic("unexpected AccountCapabilities")
}
func (stub *oauthAccountRepositoryStub) ReplaceAccountCapabilities(context.Context, tenancy.Scope, string, int64, sdk.Manifest, []sdk.Capability) (sdk.Account, []sdk.AccountCapabilitySetting, error) {
	panic("unexpected ReplaceAccountCapabilities")
}
func (stub *oauthAccountRepositoryStub) HealthHistory(context.Context, tenancy.Scope, string, int) ([]connectorrepo.HealthSnapshot, error) {
	panic("unexpected HealthHistory")
}

type oauthSessionStoreStub struct{ session *connectorauth.Session }

func (stub *oauthSessionStoreStub) CreateOrReplay(_ context.Context, _ tenancy.Scope, proposed connectorauth.Session) (connectorauth.Session, bool, error) {
	if stub.session == nil {
		stored := proposed
		stub.session = &stored
		return stored, false, nil
	}
	if stub.session.ActorID != proposed.ActorID || stub.session.CorrelationID != proposed.CorrelationID || stub.session.AccountID != proposed.AccountID || stub.session.CallbackURL != proposed.CallbackURL {
		return connectorauth.Session{}, false, connectorauth.ErrSessionConflict
	}
	return *stub.session, true, nil
}
func (stub *oauthSessionStoreStub) Consume(_ context.Context, _ tenancy.Scope, digest, actorID, callbackURL string, now time.Time) (connectorauth.Session, error) {
	if stub.session == nil || stub.session.Status != "pending" || stub.session.StateDigest != digest || stub.session.ActorID != actorID || stub.session.CallbackURL != callbackURL || now.After(stub.session.ExpiresAt) {
		return connectorauth.Session{}, connectorauth.ErrSessionConflict
	}
	consumed := now.UTC()
	stub.session.Status = "consumed"
	stub.session.ConsumedAt = &consumed
	return *stub.session, nil
}

type oauthSecretsStub struct {
	scope   tenancy.Scope
	next    int
	values  map[secrets.Reference][]byte
	classes map[secrets.Reference]secrets.Class
	revoked map[secrets.Reference]bool
}

func newOAuthSecretsStub(scope tenancy.Scope, reference secrets.Reference, material []byte) *oauthSecretsStub {
	return &oauthSecretsStub{scope: scope, next: 2, values: map[secrets.Reference][]byte{reference: append([]byte(nil), material...)}, classes: map[secrets.Reference]secrets.Class{reference: secrets.ClassOAuthClient}, revoked: map[secrets.Reference]bool{}}
}
func (stub *oauthSecretsStub) metadata(reference secrets.Reference) secrets.Metadata {
	return secrets.Metadata{Reference: reference, OrganizationID: stub.scope.OrganizationID(), WorkspaceID: stub.scope.WorkspaceID(), Class: stub.classes[reference], Status: secrets.StatusActive, CurrentVersion: 1, CreatedAt: time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)}
}
func (stub *oauthSecretsStub) Create(_ context.Context, _ tenancy.Scope, class secrets.Class, material []byte) (secrets.Metadata, error) {
	reference := secrets.Reference(fmt.Sprintf("sec:v1:%032x", stub.next))
	stub.next++
	stub.values[reference] = append([]byte(nil), material...)
	stub.classes[reference] = class
	return stub.metadata(reference), nil
}
func (stub *oauthSecretsStub) Use(_ context.Context, _ tenancy.Scope, reference secrets.Reference, consumer func([]byte) error) error {
	value, ok := stub.values[reference]
	if !ok {
		return secrets.ErrNotFound
	}
	if stub.revoked[reference] {
		return secrets.ErrRevoked
	}
	return consumer(append([]byte(nil), value...))
}
func (stub *oauthSecretsStub) Describe(_ context.Context, _ tenancy.Scope, reference secrets.Reference) (secrets.Metadata, error) {
	if _, ok := stub.values[reference]; !ok {
		return secrets.Metadata{}, secrets.ErrNotFound
	}
	return stub.metadata(reference), nil
}
func (stub *oauthSecretsStub) Rotate(_ context.Context, _ tenancy.Scope, reference secrets.Reference, material []byte) (secrets.Metadata, error) {
	if _, ok := stub.values[reference]; !ok {
		return secrets.Metadata{}, secrets.ErrNotFound
	}
	stub.values[reference] = append([]byte(nil), material...)
	return stub.metadata(reference), nil
}
func (stub *oauthSecretsStub) Revoke(_ context.Context, _ tenancy.Scope, reference secrets.Reference) (secrets.Metadata, error) {
	if _, ok := stub.values[reference]; !ok {
		return secrets.Metadata{}, secrets.ErrNotFound
	}
	stub.revoked[reference] = true
	metadata := stub.metadata(reference)
	metadata.Status = secrets.StatusRevoked
	return metadata, nil
}

type oauthAuditStub struct{ actions []string }

func (stub *oauthAuditStub) Capture(_ context.Context, _ tenancy.Scope, entry audit.Entry) (audit.Record, error) {
	stub.actions = append(stub.actions, entry.Action)
	return audit.Record{}, nil
}

func oauthAPIRequest(t *testing.T, path, body, key string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	ctx := context.WithValue(request.Context(), requestScopeKey{}, validTestScope(t))
	ctx = context.WithValue(ctx, requestIdentityKey{}, Principal{Issuer: "https://id.example.test", Subject: "admin|opaque"})
	return request.WithContext(ctx)
}

func TestConnectorOAuthStartIsIdempotentAndCallbackIsOneTime(t *testing.T) {
	scope := validTestScope(t)
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	clientReference := secrets.Reference("sec:v1:00000000000000000000000000000001")
	repository := &oauthAccountRepositoryStub{account: sdk.Account{ID: "cabinet-avito", OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), ConnectorID: strings.Join([]string{"avi", "to"}, ""), Family: sdk.FamilyClassified, Status: sdk.AccountDisabled, SecretReference: sdk.SecretReference(clientReference), Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}}
	secretStore := newOAuthSecretsStub(scope, clientReference, []byte(`{"client_id":"client-id","client_secret":"client-secret"}`))
	sessionStore := &oauthSessionStoreStub{}
	auditor := &oauthAuditStub{}
	policy, err := connectorauth.NewCallbackPolicy([]string{"https://console.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	exchanges := 0
	callbackPath := connectorauth.CallbackPath
	api := &connectorAccountAPI{repository: repository, audit: auditor, secrets: secretStore, oauthStore: sessionStore, callbacks: policy, now: func() time.Time { return now }, exchange: func(_ context.Context, _ sdk.OAuth2Configuration, client connectorauth.OAuthClient, code, callbackURL, verifier string, _ time.Duration) ([]byte, error) {
		exchanges++
		if client.ClientID != "client-id" || code != "authorization-code" || callbackURL != "https://console.example.test"+callbackPath || len(verifier) < 43 {
			return nil, errors.New("unexpected exchange input")
		}
		return []byte(`{"access_token":"opaque-access","refresh_token":"opaque-refresh","token_type":"Bearer"}`), nil
	}}

	startBody := `{"account_id":"cabinet-avito","expected_version":1,"callback_url":"https://console.example.test/oauth/connectors/callback"}`
	var first connectorOAuthStartResponse
	for attempt := 0; attempt < 2; attempt++ {
		response := httptest.NewRecorder()
		api.oauthStart(response, oauthAPIRequest(t, ConnectorOAuthStartPath, startBody, "oauth-start-1"))
		if response.Code != http.StatusOK {
			t.Fatalf("start attempt %d status=%d body=%s", attempt+1, response.Code, response.Body.String())
		}
		var result connectorOAuthStartResponse
		if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if attempt == 0 {
			first = result
		} else if result != first {
			t.Fatalf("idempotent start changed result: first=%+v second=%+v", first, result)
		}
	}
	authorizationURL, err := url.Parse(first.AuthorizationURL)
	if err != nil || authorizationURL.Query().Get("code_challenge_method") != "S256" || authorizationURL.Query().Get("state") == "" || strings.Contains(first.AuthorizationURL, "client-secret") {
		t.Fatalf("unsafe authorization URL %q err=%v", first.AuthorizationURL, err)
	}
	state := authorizationURL.Query().Get("state")
	callbackBody := fmt.Sprintf(`{"code":"authorization-code","state":%q,"callback_url":"https://console.example.test/oauth/connectors/callback"}`, state)
	response := httptest.NewRecorder()
	api.oauthCallback(response, oauthAPIRequest(t, ConnectorOAuthCallbackPath, callbackBody, "oauth-callback-1"))
	if response.Code != http.StatusOK || repository.account.Status != sdk.AccountDisabled || repository.account.Version != 2 || strings.Contains(response.Body.String(), "opaque-access") || strings.Contains(response.Body.String(), "client-secret") {
		t.Fatalf("callback status=%d account=%+v body=%s", response.Code, repository.account, response.Body.String())
	}
	replayed := httptest.NewRecorder()
	api.oauthCallback(replayed, oauthAPIRequest(t, ConnectorOAuthCallbackPath, callbackBody, "oauth-callback-2"))
	if replayed.Code != http.StatusConflict || exchanges != 1 {
		t.Fatalf("replay status=%d exchanges=%d body=%s", replayed.Code, exchanges, replayed.Body.String())
	}
	if len(auditor.actions) != 3 || auditor.actions[0] != "connector.account.oauth_started" || auditor.actions[2] != "connector.account.oauth_completed" {
		t.Fatalf("audit actions=%v", auditor.actions)
	}
}
