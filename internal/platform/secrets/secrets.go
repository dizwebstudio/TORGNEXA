// Package secrets defines TORGNEXA's only application abstraction for provider credentials.
package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const (
	maxMaterialBytes = 64 << 10
	maxKeyIDRunes    = 128
	referencePrefix  = "sec:v1:"
	algorithmAESGCM  = "aes-256-gcm"
)

var (
	ErrInvalidReference = errors.New("secrets: invalid reference")
	ErrInvalidClass     = errors.New("secrets: invalid class")
	ErrInvalidMaterial  = errors.New("secrets: invalid material")
	ErrInvalidRecord    = errors.New("secrets: invalid record")
	ErrNotFound         = errors.New("secrets: not found")
	ErrRevoked          = errors.New("secrets: revoked")
	ErrConflict         = errors.New("secrets: concurrent change")
	ErrKeyUnavailable   = errors.New("secrets: master key unavailable")
	ErrUseFailed        = errors.New("secrets: consumer failed")
)

// Class describes the purpose of a secret without exposing its value.
type Class string

const (
	ClassConnectorToken          Class = "connector_token"
	ClassOAuthClient             Class = "oauth_client"
	ClassOAuthState              Class = "oauth_state"
	ClassOAuthRefresh            Class = "oauth_refresh"
	ClassERPCredential           Class = "erp_credential"
	ClassWebhookSigning          Class = "webhook_signing"
	ClassCertificate             Class = "certificate"
	ClassStorageCredential       Class = "storage_credential"
	ClassNotificationDestination Class = "notification_destination"
	ClassPrivacyExport           Class = "privacy_export"
	ClassAIProviderCredential    Class = "ai_provider_credential"
	ClassLogisticsShipment       Class = "logistics_shipment"
)

func (class Class) Valid() bool {
	switch class {
	case ClassConnectorToken, ClassOAuthClient, ClassOAuthState, ClassOAuthRefresh, ClassERPCredential,
		ClassWebhookSigning, ClassCertificate, ClassStorageCredential, ClassNotificationDestination, ClassPrivacyExport,
		ClassAIProviderCredential, ClassLogisticsShipment:
		return true
	default:
		return false
	}
}

// Reference is an opaque, non-secret handle suitable for normal application tables.
type Reference string

func ParseReference(value string) (Reference, error) {
	ref := Reference(value)
	if !ref.Valid() {
		return "", ErrInvalidReference
	}
	return ref, nil
}

func (reference Reference) String() string { return string(reference) }

func (reference Reference) Valid() bool {
	raw := string(reference)
	if len(raw) != len(referencePrefix)+32 || !strings.HasPrefix(raw, referencePrefix) {
		return false
	}
	_, err := hex.DecodeString(raw[len(referencePrefix):])
	return err == nil
}

// Status is the lifecycle state of a stable secret reference.
type Status string

const (
	StatusActive  Status = "active"
	StatusRevoked Status = "revoked"
)

// Metadata is safe to log, audit, back up and expose to management surfaces.
type Metadata struct {
	Reference      Reference
	OrganizationID tenancy.OrganizationID
	WorkspaceID    tenancy.WorkspaceID
	Class          Class
	Status         Status
	CurrentVersion uint64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	RevokedAt      *time.Time
}

// EncryptedVersion is ciphertext-only persistence material. Plaintext is never part of this type.
type EncryptedVersion struct {
	Reference      Reference
	OrganizationID tenancy.OrganizationID
	WorkspaceID    tenancy.WorkspaceID
	Version        uint64
	Algorithm      string
	KeyID          string
	Nonce          []byte
	Ciphertext     []byte
	CreatedAt      time.Time
}

// Repository persists opaque references and ciphertext versions. Implementations must be tenant scoped.
type Repository interface {
	Create(context.Context, tenancy.Scope, Metadata, EncryptedVersion) error
	Active(context.Context, tenancy.Scope, Reference) (Metadata, EncryptedVersion, error)
	Describe(context.Context, tenancy.Scope, Reference) (Metadata, error)
	Rotate(context.Context, tenancy.Scope, Reference, uint64, EncryptedVersion, time.Time) (Metadata, error)
	Revoke(context.Context, tenancy.Scope, Reference, uint64, time.Time) (Metadata, error)
}

// MasterKey contains an AES-256 key supplied from outside the database.
// The raw key is intentionally unexported and cannot be formatted accidentally.
type MasterKey struct {
	id  string
	key [32]byte
}

func NewMasterKey(id string, material []byte) (MasterKey, error) {
	if !validKeyID(id) || len(material) != 32 {
		return MasterKey{}, ErrKeyUnavailable
	}
	var key [32]byte
	copy(key[:], material)
	return MasterKey{id: id, key: key}, nil
}

func (key MasterKey) ID() string     { return key.id }
func (key MasterKey) String() string { return "MasterKey(" + key.id + ")" }

// MasterKeySource allows local keyrings, Vault, KMS, HSM or remote providers without putting keys in PostgreSQL.
type MasterKeySource interface {
	Current(context.Context) (MasterKey, error)
	ByID(context.Context, string) (MasterKey, error)
}

// SecretProvider is the only application API for secret material.
// Use executes a callback while plaintext exists in memory and wipes the provider-owned buffer immediately afterward.
type SecretProvider interface {
	Create(context.Context, tenancy.Scope, Class, []byte) (Metadata, error)
	Use(context.Context, tenancy.Scope, Reference, func([]byte) error) error
	Describe(context.Context, tenancy.Scope, Reference) (Metadata, error)
	Rotate(context.Context, tenancy.Scope, Reference, []byte) (Metadata, error)
	Revoke(context.Context, tenancy.Scope, Reference) (Metadata, error)
}

// LocalEncryptedProvider is the Community provider: AES-256-GCM at rest, with master keys supplied outside PostgreSQL.
type LocalEncryptedProvider struct {
	repository Repository
	keys       MasterKeySource
	random     io.Reader
	clock      func() time.Time
}

var _ SecretProvider = (*LocalEncryptedProvider)(nil)

func NewLocalEncryptedProvider(repository Repository, keys MasterKeySource) (*LocalEncryptedProvider, error) {
	if repository == nil || keys == nil {
		return nil, errors.New("secrets provider: repository and key source are required")
	}
	return &LocalEncryptedProvider{repository: repository, keys: keys, random: rand.Reader, clock: time.Now}, nil
}

func (local *LocalEncryptedProvider) Create(ctx context.Context, scope tenancy.Scope, class Class, material []byte) (Metadata, error) {
	if err := local.validateCall(ctx, scope); err != nil {
		return Metadata{}, err
	}
	if !class.Valid() {
		return Metadata{}, ErrInvalidClass
	}
	if !validMaterial(material) {
		return Metadata{}, ErrInvalidMaterial
	}
	reference, err := newReference(local.random)
	if err != nil {
		return Metadata{}, err
	}
	now := local.clock().UTC()
	metadata := Metadata{Reference: reference, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Class: class, Status: StatusActive, CurrentVersion: 1, CreatedAt: now, UpdatedAt: now}
	version, err := local.encrypt(ctx, metadata, 1, material, now)
	if err != nil {
		return Metadata{}, err
	}
	if err := local.repository.Create(ctx, scope, metadata, version); err != nil {
		return Metadata{}, fmt.Errorf("secrets provider: create: %w", normalizeRepositoryError(err))
	}
	return metadata, nil
}

func (local *LocalEncryptedProvider) Use(ctx context.Context, scope tenancy.Scope, reference Reference, consumer func([]byte) error) error {
	if err := local.validateCall(ctx, scope); err != nil {
		return err
	}
	if !reference.Valid() {
		return ErrInvalidReference
	}
	if consumer == nil {
		return errors.New("secrets provider: consumer is required")
	}
	metadata, version, err := local.repository.Active(ctx, scope, reference)
	if err != nil {
		return normalizeRepositoryError(err)
	}
	if err := ValidateStoredPair(scope, metadata, version); err != nil {
		return err
	}
	if metadata.Status != StatusActive {
		return ErrRevoked
	}
	plaintext, err := local.decrypt(ctx, metadata, version)
	if err != nil {
		return err
	}
	defer wipe(plaintext)
	if err := consumer(plaintext); err != nil {
		return ErrUseFailed
	}
	return nil
}

func (local *LocalEncryptedProvider) Describe(ctx context.Context, scope tenancy.Scope, reference Reference) (Metadata, error) {
	if err := local.validateCall(ctx, scope); err != nil {
		return Metadata{}, err
	}
	if !reference.Valid() {
		return Metadata{}, ErrInvalidReference
	}
	metadata, err := local.repository.Describe(ctx, scope, reference)
	if err != nil {
		return Metadata{}, normalizeRepositoryError(err)
	}
	if err := ValidateMetadata(scope, metadata); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func (local *LocalEncryptedProvider) Rotate(ctx context.Context, scope tenancy.Scope, reference Reference, material []byte) (Metadata, error) {
	if err := local.validateCall(ctx, scope); err != nil {
		return Metadata{}, err
	}
	if !reference.Valid() {
		return Metadata{}, ErrInvalidReference
	}
	if !validMaterial(material) {
		return Metadata{}, ErrInvalidMaterial
	}
	metadata, active, err := local.repository.Active(ctx, scope, reference)
	if err != nil {
		return Metadata{}, normalizeRepositoryError(err)
	}
	if err := ValidateStoredPair(scope, metadata, active); err != nil {
		return Metadata{}, err
	}
	if metadata.Status != StatusActive {
		return Metadata{}, ErrRevoked
	}
	next := metadata.CurrentVersion + 1
	if next == 0 {
		return Metadata{}, ErrInvalidRecord
	}
	now := local.clock().UTC()
	version, err := local.encrypt(ctx, metadata, next, material, now)
	if err != nil {
		return Metadata{}, err
	}
	updated, err := local.repository.Rotate(ctx, scope, reference, metadata.CurrentVersion, version, now)
	if err != nil {
		return Metadata{}, normalizeRepositoryError(err)
	}
	if err := ValidateMetadata(scope, updated); err != nil || updated.Reference != reference || updated.CurrentVersion != next {
		return Metadata{}, ErrInvalidRecord
	}
	return updated, nil
}

func (local *LocalEncryptedProvider) Revoke(ctx context.Context, scope tenancy.Scope, reference Reference) (Metadata, error) {
	if err := local.validateCall(ctx, scope); err != nil {
		return Metadata{}, err
	}
	if !reference.Valid() {
		return Metadata{}, ErrInvalidReference
	}
	metadata, err := local.repository.Describe(ctx, scope, reference)
	if err != nil {
		return Metadata{}, normalizeRepositoryError(err)
	}
	if err := ValidateMetadata(scope, metadata); err != nil {
		return Metadata{}, err
	}
	if metadata.Status == StatusRevoked {
		return metadata, nil
	}
	now := local.clock().UTC()
	updated, err := local.repository.Revoke(ctx, scope, reference, metadata.CurrentVersion, now)
	if err != nil {
		return Metadata{}, normalizeRepositoryError(err)
	}
	if updated.Status != StatusRevoked || updated.RevokedAt == nil {
		return Metadata{}, ErrInvalidRecord
	}
	return updated, nil
}

func (local *LocalEncryptedProvider) validateCall(ctx context.Context, scope tenancy.Scope) error {
	if ctx == nil {
		return errors.New("secrets provider: context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("secrets provider: %w", err)
	}
	if local == nil || local.repository == nil || local.keys == nil || local.random == nil || local.clock == nil {
		return errors.New("secrets provider: provider is not initialized")
	}
	if !scope.Valid() {
		return tenancy.ErrInvalidScope
	}
	return nil
}

func (local *LocalEncryptedProvider) encrypt(ctx context.Context, metadata Metadata, version uint64, material []byte, now time.Time) (EncryptedVersion, error) {
	key, err := local.keys.Current(ctx)
	if err != nil || !validMasterKey(key) {
		return EncryptedVersion{}, ErrKeyUnavailable
	}
	block, err := aes.NewCipher(key.key[:])
	if err != nil {
		return EncryptedVersion{}, ErrKeyUnavailable
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return EncryptedVersion{}, ErrKeyUnavailable
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(local.random, nonce); err != nil {
		return EncryptedVersion{}, errors.New("secrets provider: random source failed")
	}
	plaintext := append([]byte(nil), material...)
	defer wipe(plaintext)
	aad := associatedData(metadata.Reference, metadata.OrganizationID, metadata.WorkspaceID, metadata.Class, version, key.id)
	ciphertext := gcm.Seal(nil, nonce, plaintext, aad)
	return EncryptedVersion{Reference: metadata.Reference, OrganizationID: metadata.OrganizationID, WorkspaceID: metadata.WorkspaceID, Version: version, Algorithm: algorithmAESGCM, KeyID: key.id, Nonce: nonce, Ciphertext: ciphertext, CreatedAt: now}, nil
}

func (local *LocalEncryptedProvider) decrypt(ctx context.Context, metadata Metadata, version EncryptedVersion) ([]byte, error) {
	key, err := local.keys.ByID(ctx, version.KeyID)
	if err != nil || !validMasterKey(key) || key.id != version.KeyID {
		return nil, ErrKeyUnavailable
	}
	block, err := aes.NewCipher(key.key[:])
	if err != nil {
		return nil, ErrKeyUnavailable
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(version.Nonce) != gcm.NonceSize() {
		return nil, ErrInvalidRecord
	}
	aad := associatedData(metadata.Reference, metadata.OrganizationID, metadata.WorkspaceID, metadata.Class, version.Version, version.KeyID)
	plaintext, err := gcm.Open(nil, version.Nonce, version.Ciphertext, aad)
	if err != nil {
		return nil, ErrInvalidRecord
	}
	if !validMaterial(plaintext) {
		wipe(plaintext)
		return nil, ErrInvalidRecord
	}
	return plaintext, nil
}

func ValidateMetadata(scope tenancy.Scope, metadata Metadata) error {
	if !scope.Valid() || !metadata.Reference.Valid() || metadata.OrganizationID != scope.OrganizationID() || metadata.WorkspaceID != scope.WorkspaceID() || !metadata.Class.Valid() || metadata.CurrentVersion == 0 || metadata.CreatedAt.IsZero() || metadata.UpdatedAt.IsZero() || metadata.UpdatedAt.Before(metadata.CreatedAt) {
		return ErrInvalidRecord
	}
	switch metadata.Status {
	case StatusActive:
		if metadata.RevokedAt != nil {
			return ErrInvalidRecord
		}
	case StatusRevoked:
		if metadata.RevokedAt == nil || metadata.RevokedAt.Before(metadata.CreatedAt) {
			return ErrInvalidRecord
		}
	default:
		return ErrInvalidRecord
	}
	return nil
}

func ValidateStoredPair(scope tenancy.Scope, metadata Metadata, version EncryptedVersion) error {
	if err := ValidateMetadata(scope, metadata); err != nil {
		return err
	}
	if version.Reference != metadata.Reference || version.OrganizationID != metadata.OrganizationID || version.WorkspaceID != metadata.WorkspaceID || version.Version != metadata.CurrentVersion || version.Algorithm != algorithmAESGCM || !validKeyID(version.KeyID) || len(version.Nonce) != 12 || len(version.Ciphertext) < 17 || len(version.Ciphertext) > maxMaterialBytes+32 || version.CreatedAt.IsZero() {
		return ErrInvalidRecord
	}
	return nil
}

func validMaterial(material []byte) bool {
	return len(material) > 0 && len(material) <= maxMaterialBytes
}
func validMasterKey(key MasterKey) bool { return validKeyID(key.id) }
func validKeyID(id string) bool {
	if id == "" || id != strings.TrimSpace(id) || !utf8.ValidString(id) || utf8.RuneCountInString(id) > maxKeyIDRunes {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' || r == ':' || r == '/') {
			return false
		}
	}
	return true
}
func associatedData(reference Reference, organization tenancy.OrganizationID, workspace tenancy.WorkspaceID, class Class, version uint64, keyID string) []byte {
	return []byte(fmt.Sprintf("torgnexa.secret.v1\x00%s\x00%s\x00%s\x00%s\x00%d\x00%s", reference, organization, workspace, class, version, keyID))
}
func newReference(random io.Reader) (Reference, error) {
	if random == nil {
		return "", errors.New("secrets provider: random source failed")
	}
	raw := make([]byte, 16)
	if _, err := io.ReadFull(random, raw); err != nil {
		return "", errors.New("secrets provider: random source failed")
	}
	return Reference(referencePrefix + hex.EncodeToString(raw)), nil
}
func wipe(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
func normalizeRepositoryError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return ErrNotFound
	case errors.Is(err, ErrRevoked):
		return ErrRevoked
	case errors.Is(err, ErrConflict):
		return ErrConflict
	default:
		return err
	}
}
