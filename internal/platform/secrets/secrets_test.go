package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const (
	testOrg  = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	testWS   = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
	otherOrg = "018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001"
	otherWS  = "018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002"
)

func TestLocalEncryptedProviderLifecycle(t *testing.T) {
	t.Parallel()
	scope := mustScope(t, testOrg, testWS)
	repository := newMemoryRepository()
	keyring, err := NewStaticKeyring("community-2026-08", map[string][]byte{"community-2026-08": bytes.Repeat([]byte{0x42}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewLocalEncryptedProvider(repository, keyring)
	if err != nil {
		t.Fatal(err)
	}
	provider.random = bytes.NewReader(bytes.Repeat([]byte{0x11}, 128))
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	provider.clock = func() time.Time { return now }

	original := []byte("Bearer super-sensitive-provider-token")
	metadata, err := provider.Create(context.Background(), scope, ClassConnectorToken, original)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !metadata.Reference.Valid() || metadata.CurrentVersion != 1 || metadata.Status != StatusActive {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	if bytes.Contains(repository.active.Ciphertext, original) || bytes.Contains(repository.active.Nonce, original) {
		t.Fatal("plaintext leaked into repository record")
	}

	var observed []byte
	err = provider.Use(context.Background(), scope, metadata.Reference, func(material []byte) error {
		observed = append([]byte(nil), material...)
		return nil
	})
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	if !bytes.Equal(observed, original) {
		t.Fatalf("resolved material differs: %q", observed)
	}

	now = now.Add(time.Hour)
	rotated := []byte("rotated-token-value")
	updated, err := provider.Rotate(context.Background(), scope, metadata.Reference, rotated)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if updated.Reference != metadata.Reference || updated.CurrentVersion != 2 {
		t.Fatalf("rotation changed stable handle: %#v", updated)
	}
	if len(repository.versions[metadata.Reference]) != 2 {
		t.Fatalf("expected encrypted history, got %d versions", len(repository.versions[metadata.Reference]))
	}
	if bytes.Contains(repository.active.Ciphertext, rotated) {
		t.Fatal("rotated plaintext leaked into ciphertext")
	}

	observed = nil
	if err := provider.Use(context.Background(), scope, metadata.Reference, func(material []byte) error { observed = append([]byte(nil), material...); return nil }); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(observed, rotated) {
		t.Fatalf("rotation did not become active: %q", observed)
	}

	now = now.Add(time.Hour)
	revoked, err := provider.Revoke(context.Background(), scope, metadata.Reference)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if revoked.Status != StatusRevoked || revoked.RevokedAt == nil {
		t.Fatalf("not revoked: %#v", revoked)
	}
	if err := provider.Use(context.Background(), scope, metadata.Reference, func([]byte) error { return nil }); !errors.Is(err, ErrRevoked) {
		t.Fatalf("use revoked: %v", err)
	}
	again, err := provider.Revoke(context.Background(), scope, metadata.Reference)
	if err != nil || again.Status != StatusRevoked {
		t.Fatalf("idempotent revoke: %#v %v", again, err)
	}
}

func TestCiphertextBoundToTenantAndMetadata(t *testing.T) {
	t.Parallel()
	scope := mustScope(t, testOrg, testWS)
	repository := newMemoryRepository()
	keyring, _ := NewStaticKeyring("k1", map[string][]byte{"k1": bytes.Repeat([]byte{1}, 32)})
	provider, _ := NewLocalEncryptedProvider(repository, keyring)
	provider.random = bytes.NewReader(bytes.Repeat([]byte{0x22}, 128))
	metadata, err := provider.Create(context.Background(), scope, ClassOAuthRefresh, []byte("refresh-secret"))
	if err != nil {
		t.Fatal(err)
	}

	corrupted := repository.active
	corrupted.WorkspaceID = tenancy.WorkspaceID(otherWS)
	repository.active = corrupted
	if err := provider.Use(context.Background(), scope, metadata.Reference, func([]byte) error { return nil }); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("tampered metadata should fail closed: %v", err)
	}
}

func TestMasterKeyRotationKeepsHistoricalSecretsReadable(t *testing.T) {
	t.Parallel()
	scope := mustScope(t, testOrg, testWS)
	repository := newMemoryRepository()
	source := &mutableKeys{current: "k1", keys: map[string]MasterKey{}}
	source.keys["k1"], _ = NewMasterKey("k1", bytes.Repeat([]byte{1}, 32))
	source.keys["k2"], _ = NewMasterKey("k2", bytes.Repeat([]byte{2}, 32))
	provider, _ := NewLocalEncryptedProvider(repository, source)
	provider.random = bytes.NewReader(bytes.Repeat([]byte{0x33}, 256))
	metadata, err := provider.Create(context.Background(), scope, ClassWebhookSigning, []byte("old-material"))
	if err != nil {
		t.Fatal(err)
	}
	if repository.active.KeyID != "k1" {
		t.Fatalf("expected k1, got %s", repository.active.KeyID)
	}

	source.current = "k2"
	if _, err := provider.Rotate(context.Background(), scope, metadata.Reference, []byte("new-material")); err != nil {
		t.Fatal(err)
	}
	if repository.active.KeyID != "k2" {
		t.Fatalf("new version did not use current master key: %s", repository.active.KeyID)
	}
	if repository.versions[metadata.Reference][0].KeyID != "k1" {
		t.Fatal("historical ciphertext lost original key id")
	}
}

func TestUseWipesProviderBufferAndDoesNotReturnConsumerErrorText(t *testing.T) {
	t.Parallel()
	scope := mustScope(t, testOrg, testWS)
	repository := newMemoryRepository()
	keyring, _ := NewStaticKeyring("k1", map[string][]byte{"k1": bytes.Repeat([]byte{9}, 32)})
	provider, _ := NewLocalEncryptedProvider(repository, keyring)
	provider.random = bytes.NewReader(bytes.Repeat([]byte{0x44}, 128))
	metadata, _ := provider.Create(context.Background(), scope, ClassERPCredential, []byte("plain-secret"))

	var secretBuffer []byte
	err := provider.Use(context.Background(), scope, metadata.Reference, func(material []byte) error {
		secretBuffer = material
		return errors.New("plain-secret must not escape through error")
	})
	if !errors.Is(err, ErrUseFailed) || strings.Contains(err.Error(), "plain-secret") {
		t.Fatalf("unsafe consumer error: %v", err)
	}
	if !bytes.Equal(secretBuffer, make([]byte, len(secretBuffer))) {
		t.Fatalf("provider-owned plaintext buffer was not wiped: %q", secretBuffer)
	}
}

func TestTenantIsolationAndValidation(t *testing.T) {
	t.Parallel()
	scope := mustScope(t, testOrg, testWS)
	other := mustScope(t, otherOrg, otherWS)
	repository := newMemoryRepository()
	keyring, _ := NewStaticKeyring("k1", map[string][]byte{"k1": bytes.Repeat([]byte{7}, 32)})
	provider, _ := NewLocalEncryptedProvider(repository, keyring)
	provider.random = bytes.NewReader(bytes.Repeat([]byte{0x55}, 128))
	metadata, err := provider.Create(context.Background(), scope, ClassStorageCredential, []byte("storage-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Use(context.Background(), other, metadata.Reference, func([]byte) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant resolve should be hidden: %v", err)
	}

	if _, err := provider.Create(context.Background(), scope, Class("unknown"), []byte("x")); !errors.Is(err, ErrInvalidClass) {
		t.Fatalf("invalid class: %v", err)
	}
	if _, err := provider.Create(context.Background(), scope, ClassConnectorToken, nil); !errors.Is(err, ErrInvalidMaterial) {
		t.Fatalf("empty secret: %v", err)
	}
	if _, err := ParseReference("raw-token-here"); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("invalid reference: %v", err)
	}
}

func TestMasterKeyFormattingNeverLeaksMaterial(t *testing.T) {
	t.Parallel()
	raw := bytes.Repeat([]byte{0xab}, 32)
	key, err := NewMasterKey("community-key", raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, formatted := range []string{fmt.Sprint(key), fmt.Sprintf("%+v", key), fmt.Sprintf("%#v", key)} {
		if strings.Contains(strings.ToLower(formatted), "abababab") || !strings.Contains(formatted, "community-key") {
			t.Fatalf("unsafe key formatting: %q", formatted)
		}
	}
}

func TestRedactionHelpers(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"Authorization", "client_secret", "refresh-token", "vendorApiKey", "session.id", "encryptedPrivateKey", "certificate_private_material"} {
		if !SensitiveKey(key) {
			t.Errorf("expected sensitive key %q", key)
		}
	}
	if SensitiveKey("token_count") {
		t.Fatal("token_count metric must remain visible")
	}
	for _, value := range []string{"Bearer abc", "Basic Zm9vOmJhcg==", "-----BEGIN ENCRYPTED PRIVATE KEY-----\nabc", "https://example.invalid/callback?access_token=abc", "eyJhbGciOiJub25lIn0.eyJzdWIiOiIxIn0.signature"} {
		if !SensitiveString(value) || RedactText(value) != RedactedValue {
			t.Errorf("value not redacted: %q", value)
		}
	}
	if RedactText("healthy") != "healthy" {
		t.Fatal("ordinary text was redacted")
	}
}

func mustScope(t *testing.T, org, workspace string) tenancy.Scope {
	t.Helper()
	organizationID, err := tenancy.ParseOrganizationID(org)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID, err := tenancy.ParseWorkspaceID(workspace)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := tenancy.NewScope(organizationID, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

type mutableKeys struct {
	current string
	keys    map[string]MasterKey
}

func (source *mutableKeys) Current(context.Context) (MasterKey, error) {
	key, ok := source.keys[source.current]
	if !ok {
		return MasterKey{}, ErrKeyUnavailable
	}
	return key, nil
}
func (source *mutableKeys) ByID(_ context.Context, id string) (MasterKey, error) {
	key, ok := source.keys[id]
	if !ok {
		return MasterKey{}, ErrKeyUnavailable
	}
	return key, nil
}

type memoryRepository struct {
	mu       sync.Mutex
	metadata Metadata
	active   EncryptedVersion
	versions map[Reference][]EncryptedVersion
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{versions: map[Reference][]EncryptedVersion{}}
}
func (repo *memoryRepository) Create(_ context.Context, scope tenancy.Scope, metadata Metadata, version EncryptedVersion) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.metadata.Reference != "" {
		return ErrConflict
	}
	if metadata.OrganizationID != scope.OrganizationID() || metadata.WorkspaceID != scope.WorkspaceID() {
		return ErrInvalidRecord
	}
	repo.metadata, repo.active = cloneMetadata(metadata), cloneVersion(version)
	repo.versions[metadata.Reference] = append(repo.versions[metadata.Reference], cloneVersion(version))
	return nil
}
func (repo *memoryRepository) Active(_ context.Context, scope tenancy.Scope, reference Reference) (Metadata, EncryptedVersion, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.metadata.Reference != reference || repo.metadata.OrganizationID != scope.OrganizationID() || repo.metadata.WorkspaceID != scope.WorkspaceID() {
		return Metadata{}, EncryptedVersion{}, ErrNotFound
	}
	if repo.metadata.Status == StatusRevoked {
		return Metadata{}, EncryptedVersion{}, ErrRevoked
	}
	return cloneMetadata(repo.metadata), cloneVersion(repo.active), nil
}
func (repo *memoryRepository) Describe(_ context.Context, scope tenancy.Scope, reference Reference) (Metadata, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.metadata.Reference != reference || repo.metadata.OrganizationID != scope.OrganizationID() || repo.metadata.WorkspaceID != scope.WorkspaceID() {
		return Metadata{}, ErrNotFound
	}
	return cloneMetadata(repo.metadata), nil
}
func (repo *memoryRepository) Rotate(_ context.Context, scope tenancy.Scope, reference Reference, expected uint64, version EncryptedVersion, now time.Time) (Metadata, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.metadata.Reference != reference || repo.metadata.OrganizationID != scope.OrganizationID() || repo.metadata.WorkspaceID != scope.WorkspaceID() {
		return Metadata{}, ErrNotFound
	}
	if repo.metadata.Status != StatusActive {
		return Metadata{}, ErrRevoked
	}
	if repo.metadata.CurrentVersion != expected || version.Version != expected+1 {
		return Metadata{}, ErrConflict
	}
	repo.active = cloneVersion(version)
	repo.versions[reference] = append(repo.versions[reference], cloneVersion(version))
	repo.metadata.CurrentVersion++
	repo.metadata.UpdatedAt = now
	return cloneMetadata(repo.metadata), nil
}
func (repo *memoryRepository) Revoke(_ context.Context, scope tenancy.Scope, reference Reference, expected uint64, now time.Time) (Metadata, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.metadata.Reference != reference || repo.metadata.OrganizationID != scope.OrganizationID() || repo.metadata.WorkspaceID != scope.WorkspaceID() {
		return Metadata{}, ErrNotFound
	}
	if repo.metadata.CurrentVersion != expected {
		return Metadata{}, ErrConflict
	}
	repo.metadata.Status = StatusRevoked
	repo.metadata.UpdatedAt = now
	revoked := now
	repo.metadata.RevokedAt = &revoked
	return cloneMetadata(repo.metadata), nil
}
func cloneMetadata(input Metadata) Metadata {
	output := input
	if input.RevokedAt != nil {
		value := *input.RevokedAt
		output.RevokedAt = &value
	}
	return output
}
func cloneVersion(input EncryptedVersion) EncryptedVersion {
	output := input
	output.Nonce = append([]byte(nil), input.Nonce...)
	output.Ciphertext = append([]byte(nil), input.Ciphertext...)
	return output
}
