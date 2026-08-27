package connectorauth

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const tokenManagerReference = secrets.Reference("sec:v1:0123456789abcdef0123456789abcdef")

type tokenSecretStore struct {
	mu       sync.Mutex
	metadata secrets.Metadata
	material []byte
	rotated  int
}

func (store *tokenSecretStore) Create(context.Context, tenancy.Scope, secrets.Class, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, errors.New("unused")
}
func (store *tokenSecretStore) Describe(_ context.Context, _ tenancy.Scope, _ secrets.Reference) (secrets.Metadata, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.metadata, nil
}
func (store *tokenSecretStore) Use(_ context.Context, _ tenancy.Scope, _ secrets.Reference, consumer func([]byte) error) error {
	store.mu.Lock()
	material := append([]byte(nil), store.material...)
	store.mu.Unlock()
	defer clear(material)
	if err := consumer(material); err != nil {
		return secrets.ErrUseFailed
	}
	return nil
}
func (store *tokenSecretStore) Rotate(_ context.Context, _ tenancy.Scope, _ secrets.Reference, material []byte) (secrets.Metadata, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.material = append(store.material[:0], material...)
	store.metadata.CurrentVersion++
	store.rotated++
	return store.metadata, nil
}
func (store *tokenSecretStore) Revoke(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	return secrets.Metadata{}, errors.New("unused")
}

type mutexRefreshCoordinator struct{ mu sync.Mutex }

func (coordinator *mutexRefreshCoordinator) WithRefreshLock(ctx context.Context, _ tenancy.Scope, _ secrets.Reference, operation func(context.Context) error) error {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	return operation(ctx)
}

func tokenManagerScope(t *testing.T) tenancy.Scope {
	t.Helper()
	scope, err := tenancy.ParseScope("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002")
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func tokenManagerStore(t *testing.T, class secrets.Class, material any) *tokenSecretStore {
	t.Helper()
	raw, err := json.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	scope := tokenManagerScope(t)
	return &tokenSecretStore{metadata: secrets.Metadata{Reference: tokenManagerReference, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Class: class, Status: secrets.StatusActive, CurrentVersion: 1}, material: raw}
}

func oauthAccount(connectorID string) sdk.Account {
	return sdk.Account{ID: "oauth-account", ConnectorID: connectorID, SecretReference: sdk.SecretReference(tokenManagerReference)}
}

func grantID(t *testing.T, grantType string) string {
	t.Helper()
	manifests, err := sdk.CatalogManifests()
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		configuration, configurationErr := OAuthConfiguration(manifest)
		if configurationErr == nil && configuration.GrantType == grantType {
			return manifest.ID
		}
	}
	t.Fatalf("no OAuth manifest for grant %q", grantType)
	return ""
}

func TestTokenManagerProjectsFreshAccessTokenOnly(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	store := tokenManagerStore(t, secrets.ClassOAuthRefresh, TokenBundle{AccessToken: "fresh-access-token-123456", RefreshToken: "refresh-token-123456", TokenType: "Bearer", ExpiresAt: now.Add(time.Hour).Format(time.RFC3339), ClientID: "client", ClientSecret: "secret"})
	manager, err := NewTokenManager(store, &mutexRefreshCoordinator{})
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return now }
	manager.refresh = func(context.Context, sdk.OAuth2Configuration, TokenBundle, time.Duration, time.Time) ([]byte, error) {
		t.Fatal("fresh token was refreshed")
		return nil, nil
	}
	var observed string
	if err := manager.UseAccessToken(context.Background(), tokenManagerScope(t), oauthAccount(grantID(t, "authorization_code")), func(token []byte) error { observed = string(token); return nil }); err != nil {
		t.Fatal(err)
	}
	if observed != "fresh-access-token-123456" || store.rotated != 0 {
		t.Fatalf("unexpected projection observed=%q rotations=%d", observed, store.rotated)
	}
}

func TestTokenManagerSerializesRefreshAndRotatesBundleOnce(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	account := oauthAccount(grantID(t, "authorization_code"))
	store := tokenManagerStore(t, secrets.ClassOAuthRefresh, TokenBundle{AccessToken: "expired-access-token-123", RefreshToken: "old-refresh-token-123", TokenType: "Bearer", ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339), ClientID: "client", ClientSecret: "secret"})
	manager, err := NewTokenManager(store, &mutexRefreshCoordinator{})
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return now }
	var refreshes atomic.Int64
	manager.refresh = func(_ context.Context, _ sdk.OAuth2Configuration, current TokenBundle, _ time.Duration, at time.Time) ([]byte, error) {
		refreshes.Add(1)
		if current.RefreshToken != "old-refresh-token-123" {
			return nil, errors.New("unexpected refresh token")
		}
		return json.Marshal(TokenBundle{AccessToken: "rotated-access-token-123", RefreshToken: "rotated-refresh-token-123", TokenType: "Bearer", ExpiresAt: at.Add(time.Hour).Format(time.RFC3339), ClientID: current.ClientID, ClientSecret: current.ClientSecret})
	}
	const callers = 12
	errorsByCaller := make(chan error, callers)
	for range callers {
		go func() {
			errorsByCaller <- manager.UseAccessToken(context.Background(), tokenManagerScope(t), account, func(token []byte) error {
				if string(token) != "rotated-access-token-123" {
					return errors.New("stale access token")
				}
				return nil
			})
		}()
	}
	for range callers {
		if err := <-errorsByCaller; err != nil {
			t.Fatal(err)
		}
	}
	if refreshes.Load() != 1 || store.rotated != 1 || store.metadata.CurrentVersion != 2 {
		t.Fatalf("refreshes=%d rotations=%d version=%d", refreshes.Load(), store.rotated, store.metadata.CurrentVersion)
	}
	bundle, err := ParseTokenBundle(store.material)
	if err != nil || bundle.RefreshToken != "rotated-refresh-token-123" {
		t.Fatalf("rotated bundle invalid: %+v %v", bundle, err)
	}
}

func TestTokenManagerFailsClosedWhenReauthorizationIsRequired(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	store := tokenManagerStore(t, secrets.ClassOAuthRefresh, TokenBundle{AccessToken: "expired-access-token-123", TokenType: "Bearer", ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339), ClientID: "client", ClientSecret: "secret"})
	manager, _ := NewTokenManager(store, &mutexRefreshCoordinator{})
	manager.now = func() time.Time { return now }
	err := manager.Prepare(context.Background(), tokenManagerScope(t), oauthAccount(grantID(t, "authorization_code")))
	if !errors.Is(err, ErrOAuthReauthorizationRequired) || store.rotated != 0 {
		t.Fatalf("got %v with %d rotations", err, store.rotated)
	}
}

func TestTokenManagerMapsRejectedRefreshToReauthorization(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	store := tokenManagerStore(t, secrets.ClassOAuthRefresh, TokenBundle{AccessToken: "expired-access-token-123", RefreshToken: "revoked-refresh-token-123", TokenType: "Bearer", ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339), ClientID: "client", ClientSecret: "secret"})
	manager, _ := NewTokenManager(store, &mutexRefreshCoordinator{})
	manager.now = func() time.Time { return now }
	manager.refresh = func(context.Context, sdk.OAuth2Configuration, TokenBundle, time.Duration, time.Time) ([]byte, error) {
		return nil, ErrOAuthRefreshRejected
	}
	err := manager.Prepare(context.Background(), tokenManagerScope(t), oauthAccount(grantID(t, "authorization_code")))
	if !errors.Is(err, ErrOAuthReauthorizationRequired) || store.rotated != 0 {
		t.Fatalf("got %v with %d rotations", err, store.rotated)
	}
}

func TestTokenManagerExchangesClientCredentialsWithoutExposingClientSecret(t *testing.T) {
	store := tokenManagerStore(t, secrets.ClassOAuthClient, OAuthClient{ClientID: "client-id", ClientSecret: "client-secret"})
	manager, _ := NewTokenManager(store, nil)
	manager.exchange = func(_ context.Context, configuration sdk.OAuth2Configuration, client OAuthClient, _, _, _ string, _ time.Duration) ([]byte, error) {
		if configuration.GrantType != "client_credentials" || client.ClientSecret != "client-secret" {
			t.Fatal("invalid client credential exchange")
		}
		return json.Marshal(TokenBundle{AccessToken: "client-access-token-123", TokenType: "Bearer", ClientID: client.ClientID, ClientSecret: client.ClientSecret})
	}
	var observed string
	err := manager.UseAccessToken(context.Background(), tokenManagerScope(t), oauthAccount(grantID(t, "client_credentials")), func(token []byte) error { observed = string(token); return nil })
	if err != nil || observed != "client-access-token-123" || store.rotated != 0 {
		t.Fatalf("observed=%q rotations=%d err=%v", observed, store.rotated, err)
	}
}

func TestParseTokenBundleRejectsUnknownOrUnsafeMaterial(t *testing.T) {
	for _, material := range []string{
		`{"access_token":"access-token-123456","token_type":"Bearer","client_id":"client","client_secret":"secret","unknown":"value"}`,
		`{"access_token":"access-token-123456","token_type":"Bearer value","client_id":"client","client_secret":"secret"}`,
		`{"access_token":"access-token-123456","token_type":"Bearer","client_id":"client","client_secret":"secret","expires_at":"not-a-time"}`,
	} {
		if _, err := ParseTokenBundle([]byte(material)); err == nil {
			t.Fatalf("unsafe bundle accepted: %s", material)
		}
	}
}

func TestOAuthRefreshProtocolPreservesOrRotatesRefreshToken(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	current := TokenBundle{AccessToken: "old-access-token-123", RefreshToken: "old-refresh-token-123", TokenType: "Bearer", ClientID: "client-id", ClientSecret: "client-secret"}
	form := oauthRefreshForm(current)
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != current.RefreshToken || form.Get("client_secret") != current.ClientSecret || len(form) != 4 {
		t.Fatalf("unexpected refresh form: %#v", form)
	}
	material, err := refreshedTokenBundle(current, []byte(`{"access_token":"new-access-token-123","expires_in":3600}`), now)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := ParseTokenBundle(material)
	if err != nil || bundle.RefreshToken != current.RefreshToken || bundle.ExpiresAt != now.Add(time.Hour).Format(time.RFC3339) {
		t.Fatalf("preserved refresh bundle invalid: %+v %v", bundle, err)
	}
	material, err = refreshedTokenBundle(current, []byte(`{"access_token":"newer-access-token-123","refresh_token":"new-refresh-token-123","token_type":"bearer","expires_in":7200}`), now)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err = ParseTokenBundle(material)
	if err != nil || bundle.RefreshToken != "new-refresh-token-123" || bundle.TokenType != "bearer" {
		t.Fatalf("rotated refresh bundle invalid: %+v %v", bundle, err)
	}
}

func TestOAuthRefreshStatusClassification(t *testing.T) {
	for _, status := range []int{400, 401, 403} {
		if !refreshRejectedStatus(status) {
			t.Fatalf("status %d was not classified as rejected", status)
		}
	}
	for _, status := range []int{408, 429, 500, 503} {
		if refreshRejectedStatus(status) {
			t.Fatalf("temporary status %d was classified as rejected", status)
		}
	}
}
