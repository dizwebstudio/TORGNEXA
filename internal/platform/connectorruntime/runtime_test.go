package connectorruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

type fakeSecretProvider struct {
	usedScope tenancy.Scope
	usedRef   secrets.Reference
}

func (fake *fakeSecretProvider) Create(context.Context, tenancy.Scope, secrets.Class, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, errors.New("unused")
}
func (fake *fakeSecretProvider) Describe(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
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
	material := []byte("synthetic-secret")
	defer func() {
		for i := range material {
			material[i] = 0
		}
	}()
	return consumer(material)
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
