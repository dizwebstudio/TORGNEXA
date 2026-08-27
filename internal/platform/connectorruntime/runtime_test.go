package connectorruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/connectorauth"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

type fakeSecretProvider struct {
	usedScope tenancy.Scope
	usedRef   secrets.Reference
	metadata  secrets.Metadata
	material  []byte
}

func (fake *fakeSecretProvider) Create(context.Context, tenancy.Scope, secrets.Class, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, errors.New("unused")
}
func (fake *fakeSecretProvider) Describe(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	if fake.metadata.Reference.Valid() {
		return fake.metadata, nil
	}
	return secrets.Metadata{}, errors.New("unused")
}
func (fake *fakeSecretProvider) Rotate(context.Context, tenancy.Scope, secrets.Reference, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, errors.New("unused")
}
func (fake *fakeSecretProvider) Revoke(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	return secrets.Metadata{}, errors.New("unused")
}
func (fake *fakeSecretProvider) Use(_ context.Context, scope tenancy.Scope, reference secrets.Reference, consumer func([]byte) error) error {
	fake.usedScope, fake.usedRef = scope, reference
	material := append([]byte(nil), fake.material...)
	if len(material) == 0 {
		material = []byte("synthetic-secret")
	}
	defer func() {
		for i := range material {
			material[i] = 0
		}
	}()
	return consumer(material)
}

func TestAccountRuntimeProjectsOAuthBundleToAccessToken(t *testing.T) {
	scope, err := tenancy.ParseScope("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002")
	if err != nil {
		t.Fatal(err)
	}
	reference := secrets.Reference("sec:v1:0123456789abcdef0123456789abcdef")
	secretStore := &fakeSecretProvider{
		metadata: secrets.Metadata{Reference: reference, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Class: secrets.ClassOAuthRefresh, Status: secrets.StatusActive, CurrentVersion: 1},
		material: []byte(`{"access_token":"oauth-access-token-123456","refresh_token":"oauth-refresh-token-123456","token_type":"Bearer","expires_at":"2030-01-01T00:00:00Z","client_id":"client","client_secret":"secret"}`),
	}
	account := sdk.Account{ID: "oauth-account", ConnectorID: authorizationCodeManifestID(t), SecretReference: sdk.SecretReference(reference)}
	runtime, err := NewForAccount(secretStore, nil, scope, account)
	if err != nil {
		t.Fatal(err)
	}
	var observed string
	err = runtime.Secrets().UseSecret(context.Background(), account.SecretReference, func(material []byte) error {
		observed = string(material)
		return nil
	})
	if err != nil || observed != "oauth-access-token-123456" {
		t.Fatalf("OAuth projection observed=%q err=%v", observed, err)
	}
}

func authorizationCodeManifestID(t *testing.T) string {
	t.Helper()
	manifests, err := sdk.CatalogManifests()
	if err != nil {
		t.Fatal(err)
	}
	id := ""
	for _, manifest := range manifests {
		configuration, configurationErr := connectorauth.OAuthConfiguration(manifest)
		eligible := configurationErr == nil
		if !eligible {
			continue
		}
		if configuration.GrantType == "authorization_code" {
			id = manifest.ID
			break
		}
	}
	if id == "" {
		t.Fatal("authorization-code manifest missing")
	}
	return id
}

func TestRuntimeScopesSecretAccessAndConvertsOpaqueReference(t *testing.T) {
	scope, err := tenancy.ParseScope("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002")
	if err != nil {
		t.Fatal(err)
	}
	secretStore := &fakeSecretProvider{}
	runtime, err := New(secretStore, scope)
	if err != nil {
		t.Fatal(err)
	}
	var observed string
	err = runtime.Secrets().UseSecret(context.Background(), sdk.SecretReference("sec:v1:0123456789abcdef0123456789abcdef"), func(material []byte) error {
		observed = string(material)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if observed != "synthetic-secret" || secretStore.usedScope != scope || secretStore.usedRef.String() != "sec:v1:0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected bridge result observed=%q scope=%#v ref=%q", observed, secretStore.usedScope, secretStore.usedRef)
	}
	if err := runtime.Secrets().UseSecret(context.Background(), "Bearer plaintext", func([]byte) error { return nil }); !errors.Is(err, sdk.ErrSecretReference) {
		t.Fatalf("plaintext secret reference accepted: %v", err)
	}
}
