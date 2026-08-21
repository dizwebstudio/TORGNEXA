// Package uploads implements the quarantine-first upload security boundary used by
// import, media, compliance and plugin consumers. Uploads remain inaccessible to
// downstream consumers until the complete validation and malware pipeline records
// immutable security evidence and authorizes RELEASED state.
package uploads

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const (
	DefaultMaxFileBytes int64 = 100 * 1024 * 1024
	maxFilenameRunes          = 512
	maxMediaTypeBytes         = 255
)

var (
	ErrInvalid                    = errors.New("uploads: invalid value")
	ErrNotFound                   = errors.New("uploads: not found")
	ErrConflict                   = errors.New("uploads: conflict")
	ErrNotReleased                = errors.New("uploads: object is not released")
	ErrSecurityPipelineIncomplete = errors.New("uploads: security pipeline is incomplete")
	ErrSecurityRejected           = errors.New("uploads: content rejected by security policy")
	ErrScannerUnavailable         = errors.New("uploads: malware scanner unavailable")
	ErrStorage                    = errors.New("uploads: storage failure")

	uploadIDPattern           = regexp.MustCompile(`^upl_[0-9a-f]{32}$`)
	securityEvidenceIDPattern = regexp.MustCompile(`^uev_[0-9a-f]{32}$`)
	sha256Pattern             = regexp.MustCompile(`^[0-9a-f]{64}$`)
	mediaTypePattern          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9!#$&^_.+\-/]{0,126}/[A-Za-z0-9][A-Za-z0-9!#$&^_.+\-]{0,126}$`)
	safeEventIDPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	sourcePattern             = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)
)

// ID is an opaque cryptographically random upload identity. Storage paths are
// derived from this ID and tenant scope; client filenames are never storage keys.
type ID string

func (id ID) String() string { return string(id) }
func (id ID) Valid() bool    { return uploadIDPattern.MatchString(string(id)) }

// State is the canonical upload lifecycle. The complete security pipeline permits
// forward validation/scan/release transitions plus an explicit terminal-state
// reset to QUARANTINED for re-scan.
type State string

const (
	StateReceived    State = "received"
	StateQuarantined State = "quarantined"
	StateValidated   State = "validated"
	StateScanning    State = "scanning"
	StateClean       State = "clean"
	StateRejected    State = "rejected"
	StateReleased    State = "released"
)

func (state State) Valid() bool {
	switch state {
	case StateReceived, StateQuarantined, StateValidated, StateScanning, StateClean, StateRejected, StateReleased:
		return true
	default:
		return false
	}
}

// Metadata is untrusted descriptive input. It is stored for later validation,
// but never interpreted as a filesystem/object-store path in Task 088a.
type Metadata struct {
	OriginalFilename  string
	DeclaredMediaType string
	DeclaredSizeBytes int64
}

func (metadata Metadata) Validate(maxBytes int64) error {
	if maxBytes <= 0 || metadata.DeclaredSizeBytes < 0 || metadata.DeclaredSizeBytes > maxBytes {
		return ErrInvalid
	}
	if metadata.OriginalFilename != strings.TrimSpace(metadata.OriginalFilename) || metadata.OriginalFilename == "" || !utf8.ValidString(metadata.OriginalFilename) || len([]rune(metadata.OriginalFilename)) > maxFilenameRunes {
		return ErrInvalid
	}
	for _, r := range metadata.OriginalFilename {
		if unicode.IsControl(r) {
			return ErrInvalid
		}
	}
	if metadata.DeclaredMediaType != "" && (len(metadata.DeclaredMediaType) > maxMediaTypeBytes || !mediaTypePattern.MatchString(metadata.DeclaredMediaType)) {
		return ErrInvalid
	}
	return nil
}

// StoredObject is storage adapter output. The service verifies its key, size and
// checksum before a record can become QUARANTINED.
type StoredObject struct {
	Key       string
	SizeBytes int64
	SHA256    string
}

func (object StoredObject) validFor(expectedKey string, maxBytes int64) bool {
	return object.Key == expectedKey && object.SizeBytes >= 0 && object.SizeBytes <= maxBytes && sha256Pattern.MatchString(object.SHA256)
}

// ValidateFor verifies that a storage adapter returned exactly the server-derived
// tenant key and bounded immutable content metadata for this upload.
func (object StoredObject) ValidateFor(scope tenancy.Scope, id ID, maxBytes int64) error {
	if !scope.Valid() || !id.Valid() || !object.validFor(QuarantineObjectKey(scope, id), maxBytes) {
		return ErrInvalid
	}
	return nil
}

// ValidateReleasedFor verifies that a release-store adapter returned exactly the
// server-derived released key and the bounded immutable content metadata.
func (object StoredObject) ValidateReleasedFor(scope tenancy.Scope, id ID, maxBytes int64) error {
	if !scope.Valid() || !id.Valid() || !object.validFor(ReleasedObjectKey(scope, id), maxBytes) {
		return ErrInvalid
	}
	return nil
}

// Record is the canonical tenant-scoped upload metadata projection.
type Record struct {
	ID                  ID
	OrganizationID      tenancy.OrganizationID
	WorkspaceID         tenancy.WorkspaceID
	Metadata            Metadata
	State               State
	QuarantineObjectKey string
	ReleasedObjectKey   string
	ContentSizeBytes    int64
	ContentSHA256       string
	SecurityEvidenceID  string
	Version             int64
	ReceivedAt          time.Time
	QuarantinedAt       *time.Time
	ReleasedAt          *time.Time
	UpdatedAt           time.Time
}

func (record Record) Validate(scope tenancy.Scope, maxBytes int64) error {
	if !scope.Valid() || !record.ID.Valid() || record.OrganizationID != scope.OrganizationID() || record.WorkspaceID != scope.WorkspaceID() || record.Metadata.Validate(maxBytes) != nil || !record.State.Valid() || record.Version < 1 || !isUTC(record.ReceivedAt) || !isUTC(record.UpdatedAt) || record.UpdatedAt.Before(record.ReceivedAt) {
		return ErrInvalid
	}
	qKey := QuarantineObjectKey(scope, record.ID)
	rKey := ReleasedObjectKey(scope, record.ID)
	switch record.State {
	case StateReceived:
		if record.QuarantineObjectKey != "" || record.ReleasedObjectKey != "" || record.ContentSizeBytes != 0 || record.ContentSHA256 != "" || record.SecurityEvidenceID != "" || record.QuarantinedAt != nil || record.ReleasedAt != nil {
			return ErrInvalid
		}
	case StateQuarantined, StateValidated, StateScanning:
		if record.QuarantineObjectKey != qKey || record.ReleasedObjectKey != "" || record.ContentSizeBytes < 0 || record.ContentSizeBytes > maxBytes || !sha256Pattern.MatchString(record.ContentSHA256) || record.SecurityEvidenceID != "" || record.QuarantinedAt == nil || !isUTC(*record.QuarantinedAt) || record.QuarantinedAt.Before(record.ReceivedAt) || record.ReleasedAt != nil {
			return ErrInvalid
		}
	case StateClean, StateRejected:
		if record.QuarantineObjectKey != qKey || record.ReleasedObjectKey != "" || record.ContentSizeBytes < 0 || record.ContentSizeBytes > maxBytes || !sha256Pattern.MatchString(record.ContentSHA256) || !securityEvidenceIDPattern.MatchString(record.SecurityEvidenceID) || record.QuarantinedAt == nil || !isUTC(*record.QuarantinedAt) || record.ReleasedAt != nil {
			return ErrInvalid
		}
	case StateReleased:
		if record.QuarantineObjectKey != qKey || record.ReleasedObjectKey != rKey || record.ContentSizeBytes < 0 || record.ContentSizeBytes > maxBytes || !sha256Pattern.MatchString(record.ContentSHA256) || !securityEvidenceIDPattern.MatchString(record.SecurityEvidenceID) || record.QuarantinedAt == nil || !isUTC(*record.QuarantinedAt) || record.ReleasedAt == nil || !isUTC(*record.ReleasedAt) || record.ReleasedAt.Before(*record.QuarantinedAt) {
			return ErrInvalid
		}
	}
	return nil
}

// Mutation supplies the immutable event metadata used when quarantine becomes
// durable in PostgreSQL and the corresponding outbox event is committed.
type Mutation struct {
	EventID       string
	OccurredAt    time.Time
	Source        string
	CorrelationID string
	CausationID   string
	ActorID       string
	TraceID       string
}

func (mutation Mutation) Validate() error {
	if !safeEventIDPattern.MatchString(mutation.EventID) || !isUTC(mutation.OccurredAt) || !sourcePattern.MatchString(mutation.Source) {
		return ErrInvalid
	}
	for _, value := range []string{mutation.CorrelationID, mutation.CausationID, mutation.ActorID, mutation.TraceID} {
		if value != "" && !safeEventIDPattern.MatchString(value) {
			return ErrInvalid
		}
	}
	return nil
}

// Repository is the read/foundation boundary shared by admission and consumers.
// SecurityPipelineRepository below owns every transition beyond quarantine.
type Repository interface {
	CreateReceived(context.Context, tenancy.Scope, Record) error
	MarkQuarantined(context.Context, tenancy.Scope, ID, StoredObject, Mutation) (Record, error)
	Get(context.Context, tenancy.Scope, ID) (Record, error)
}

// QuarantineStore receives untrusted bytes under a server-derived key. Adapters
// must enforce maxBytes while streaming and return the actual SHA-256 and size.
type QuarantineStore interface {
	PutQuarantined(context.Context, tenancy.Scope, ID, io.Reader, int64) (StoredObject, error)
}

// ReleaseStore promotes a security-approved immutable object from quarantine to
// the server-derived released path. Implementations must verify source SHA-256 and
// must never accept a client-controlled destination key.
type ReleaseStore interface {
	Promote(context.Context, tenancy.Scope, ID, string, string) (StoredObject, error)
}

// ReleasedObject is a bounded byte-stream view of an immutable released object.
type ReleasedObject interface {
	io.Reader
	io.Closer
}

// ReleaseReader opens only the server-derived released object for an upload.
// Callers must independently confirm release via AccessGate before opening;
// the key alone is not proof of authorization.
type ReleaseReader interface {
	OpenReleased(context.Context, tenancy.Scope, ID, string) (ReleasedObject, error)
}

// ReleasedObjectRef is the only object reference downstream consumers may use.
// Fields are private so callers cannot manufacture a released reference.
type ReleasedObjectRef struct {
	uploadID      ID
	objectKey     string
	sizeBytes     int64
	sha256        string
	evidenceID    string
	recordVersion int64
}

func (ref ReleasedObjectRef) UploadID() ID         { return ref.uploadID }
func (ref ReleasedObjectRef) ObjectKey() string    { return ref.objectKey }
func (ref ReleasedObjectRef) SizeBytes() int64     { return ref.sizeBytes }
func (ref ReleasedObjectRef) SHA256() string       { return ref.sha256 }
func (ref ReleasedObjectRef) EvidenceID() string   { return ref.evidenceID }
func (ref ReleasedObjectRef) RecordVersion() int64 { return ref.recordVersion }
func (ref ReleasedObjectRef) Valid() bool {
	return ref.uploadID.Valid() && ref.objectKey != "" && ref.sizeBytes >= 0 && sha256Pattern.MatchString(ref.sha256) && securityEvidenceIDPattern.MatchString(ref.evidenceID) && ref.recordVersion >= 1
}

// Policy is the executable subset of contracts/upload/upload-policy.yaml.
// All limits are explicit so archive/parser work remains bounded before a file
// can become consumable. Scanner failure mode is always fail-closed: retry keeps
// the record in SCANNING; reject records an immutable rejected decision.
type ScannerFailureMode string

const (
	ScannerFailureRetry  ScannerFailureMode = "retry"
	ScannerFailureReject ScannerFailureMode = "reject"
)

func (mode ScannerFailureMode) Valid() bool {
	return mode == ScannerFailureRetry || mode == ScannerFailureReject
}

type Policy struct {
	Version                 string
	MaxFileBytes            int64
	MaxArchiveEntries       int
	MaxArchiveDepth         int
	MaxArchiveExpandedBytes int64
	MaxArchiveEntryBytes    int64
	MaxNestedArchiveBytes   int64
	MaxExpansionRatio       int64
	MaxParserDepth          int
	MaxParserTokens         int
	MaxCSVRows              int
	MaxCSVColumns           int
	MaxFieldBytes           int
	ScannerFailureMode      ScannerFailureMode
}

func DefaultPolicy() Policy {
	return Policy{
		Version:                 "upload-security-v1",
		MaxFileBytes:            DefaultMaxFileBytes,
		MaxArchiveEntries:       10000,
		MaxArchiveDepth:         5,
		MaxArchiveExpandedBytes: 512 * 1024 * 1024,
		MaxArchiveEntryBytes:    128 * 1024 * 1024,
		MaxNestedArchiveBytes:   32 * 1024 * 1024,
		MaxExpansionRatio:       100,
		MaxParserDepth:          64,
		MaxParserTokens:         1_000_000,
		MaxCSVRows:              100_000,
		MaxCSVColumns:           512,
		MaxFieldBytes:           1 * 1024 * 1024,
		ScannerFailureMode:      ScannerFailureRetry,
	}
}
func (policy Policy) Validate() error {
	if !sourcePattern.MatchString(policy.Version) || policy.MaxFileBytes <= 0 || policy.MaxFileBytes > 10*1024*1024*1024 ||
		policy.MaxArchiveEntries < 1 || policy.MaxArchiveEntries > 1_000_000 || policy.MaxArchiveDepth < 1 || policy.MaxArchiveDepth > 16 ||
		policy.MaxArchiveExpandedBytes < policy.MaxFileBytes || policy.MaxArchiveExpandedBytes > 20*1024*1024*1024 ||
		policy.MaxArchiveEntryBytes < 1 || policy.MaxArchiveEntryBytes > policy.MaxArchiveExpandedBytes ||
		policy.MaxNestedArchiveBytes < 1 || policy.MaxNestedArchiveBytes > policy.MaxArchiveEntryBytes ||
		policy.MaxExpansionRatio < 1 || policy.MaxExpansionRatio > 10000 ||
		policy.MaxParserDepth < 1 || policy.MaxParserDepth > 256 || policy.MaxParserTokens < 1 || policy.MaxParserTokens > 10_000_000 ||
		policy.MaxCSVRows < 1 || policy.MaxCSVRows > 1_000_000 || policy.MaxCSVColumns < 1 || policy.MaxCSVColumns > 4096 ||
		policy.MaxFieldBytes < 1 || policy.MaxFieldBytes > 16*1024*1024 || !policy.ScannerFailureMode.Valid() {
		return ErrInvalid
	}
	return nil
}

// Service admits bytes into quarantine. A failure after object storage succeeds
// may leave an orphaned quarantine object, but it can never become consumable;
// this deliberately chooses fail-closed security over unsafe compensation.
type Service struct {
	repository Repository
	storage    QuarantineStore
	policy     Policy
	now        func() time.Time
	random     io.Reader
}

func NewService(repository Repository, storage QuarantineStore, policy Policy) (*Service, error) {
	return newService(repository, storage, policy, time.Now, rand.Reader)
}

func newService(repository Repository, storage QuarantineStore, policy Policy, now func() time.Time, random io.Reader) (*Service, error) {
	if repository == nil || storage == nil || now == nil || random == nil || policy.Validate() != nil {
		return nil, ErrInvalid
	}
	return &Service{repository: repository, storage: storage, policy: policy, now: now, random: random}, nil
}

// Receive persists RECEIVED first, writes the object to a tenant-derived
// quarantine path, then atomically marks QUARANTINED and writes its outbox event.
func (service *Service) Receive(ctx context.Context, scope tenancy.Scope, metadata Metadata, source io.Reader, mutation Mutation) (Record, error) {
	if ctx == nil || source == nil || service == nil || service.repository == nil || service.storage == nil || !scope.Valid() || metadata.Validate(service.policy.MaxFileBytes) != nil || mutation.Validate() != nil {
		return Record{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	id, err := newID(service.random)
	if err != nil {
		return Record{}, fmt.Errorf("uploads: generate id: %w", err)
	}
	return service.receiveWithID(ctx, scope, id, metadata, source, mutation)
}

// ReceiveWithID admits an upload using a server-derived stable identifier. It
// exists for retry-safe API adapters: a repeated idempotency key resumes a
// RECEIVED object or returns the already quarantined record, while different
// metadata for the same key conflicts.
func (service *Service) ReceiveWithID(ctx context.Context, scope tenancy.Scope, id ID, metadata Metadata, source io.Reader, mutation Mutation) (Record, error) {
	if !id.Valid() {
		return Record{}, ErrInvalid
	}
	return service.receiveWithID(ctx, scope, id, metadata, source, mutation)
}

func (service *Service) receiveWithID(ctx context.Context, scope tenancy.Scope, id ID, metadata Metadata, source io.Reader, mutation Mutation) (Record, error) {
	if ctx == nil || source == nil || service == nil || service.repository == nil || service.storage == nil || !scope.Valid() || metadata.Validate(service.policy.MaxFileBytes) != nil || mutation.Validate() != nil {
		return Record{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	now := service.now().UTC()
	record := Record{ID: id, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Metadata: metadata, State: StateReceived, Version: 1, ReceivedAt: now, UpdatedAt: now}
	if err := record.Validate(scope, service.policy.MaxFileBytes); err != nil {
		return Record{}, err
	}
	if err := service.repository.CreateReceived(ctx, scope, record); err != nil {
		if !errors.Is(err, ErrConflict) {
			return Record{}, err
		}
		existing, getErr := service.repository.Get(ctx, scope, id)
		if getErr != nil || existing.Metadata != metadata {
			return Record{}, ErrConflict
		}
		if existing.State == StateQuarantined {
			return existing, nil
		}
		if existing.State != StateReceived {
			return Record{}, ErrConflict
		}
		record = existing
	}
	stored, err := service.storage.PutQuarantined(ctx, scope, id, source, service.policy.MaxFileBytes)
	if err != nil {
		return Record{}, fmt.Errorf("%w: quarantine put", ErrStorage)
	}
	if stored.ValidateFor(scope, id, service.policy.MaxFileBytes) != nil {
		return Record{}, ErrStorage
	}
	if metadata.DeclaredSizeBytes > 0 && stored.SizeBytes != metadata.DeclaredSizeBytes {
		return Record{}, ErrInvalid
	}
	mutation.OccurredAt = mutation.OccurredAt.UTC()
	if mutation.OccurredAt.Before(record.ReceivedAt) {
		mutation.OccurredAt = record.ReceivedAt
	}
	result, err := service.repository.MarkQuarantined(ctx, scope, id, stored, mutation)
	if err != nil {
		return Record{}, err
	}
	if result.Validate(scope, service.policy.MaxFileBytes) != nil || result.State != StateQuarantined {
		return Record{}, ErrInvalid
	}
	return result, nil
}

// AccessGate resolves a consumer-safe reference only after Task 088b has
// produced a RELEASED record with security evidence. Missing/cross-tenant rows
// intentionally collapse to ErrNotReleased to avoid existence disclosure.
type AccessGate struct {
	repository Repository
	policy     Policy
}

func NewAccessGate(repository Repository, policy Policy) (*AccessGate, error) {
	if repository == nil || policy.Validate() != nil {
		return nil, ErrInvalid
	}
	return &AccessGate{repository: repository, policy: policy}, nil
}

func (gate *AccessGate) ResolveReleased(ctx context.Context, scope tenancy.Scope, id ID) (ReleasedObjectRef, error) {
	if ctx == nil || gate == nil || gate.repository == nil || !scope.Valid() || !id.Valid() {
		return ReleasedObjectRef{}, ErrNotReleased
	}
	record, err := gate.repository.Get(ctx, scope, id)
	if err != nil {
		return ReleasedObjectRef{}, ErrNotReleased
	}
	if record.Validate(scope, gate.policy.MaxFileBytes) != nil || record.State != StateReleased || record.SecurityEvidenceID == "" || record.ReleasedObjectKey != ReleasedObjectKey(scope, id) {
		return ReleasedObjectRef{}, ErrNotReleased
	}
	ref := ReleasedObjectRef{uploadID: id, objectKey: record.ReleasedObjectKey, sizeBytes: record.ContentSizeBytes, sha256: record.ContentSHA256, evidenceID: record.SecurityEvidenceID, recordVersion: record.Version}
	if !ref.Valid() {
		return ReleasedObjectRef{}, ErrNotReleased
	}
	return ref, nil
}

// ValidateReleasedRef re-checks an already issued capability against current
// upload state. Consumers must call it immediately before every object read; a
// re-scan transition invalidates older references by changing state/version/evidence.
func (gate *AccessGate) ValidateReleasedRef(ctx context.Context, scope tenancy.Scope, ref ReleasedObjectRef) error {
	if ctx == nil || gate == nil || gate.repository == nil || !scope.Valid() || !ref.Valid() {
		return ErrNotReleased
	}
	record, err := gate.repository.Get(ctx, scope, ref.uploadID)
	if err != nil || record.Validate(scope, gate.policy.MaxFileBytes) != nil || record.State != StateReleased {
		return ErrNotReleased
	}
	if record.ReleasedObjectKey != ref.objectKey || record.ContentSizeBytes != ref.sizeBytes || record.ContentSHA256 != ref.sha256 || record.SecurityEvidenceID != ref.evidenceID || record.Version != ref.recordVersion {
		return ErrNotReleased
	}
	return nil
}

func QuarantineObjectKey(scope tenancy.Scope, id ID) string {
	if !scope.Valid() || !id.Valid() {
		return ""
	}
	return "quarantine/" + scope.OrganizationID().String() + "/" + scope.WorkspaceID().String() + "/" + id.String() + "/object"
}

func ReleasedObjectKey(scope tenancy.Scope, id ID) string {
	if !scope.Valid() || !id.Valid() {
		return ""
	}
	return "released/" + scope.OrganizationID().String() + "/" + scope.WorkspaceID().String() + "/" + id.String() + "/object"
}

var contentPathPattern = regexp.MustCompile(`^/api/v1/uploads/upl_[0-9a-f]{32}/content$`)

// ContentPath is the single source of truth for the server-relative path a
// released upload's bytes are served from. Consumers (e.g. a product image
// reference) store this exact shape instead of a client-supplied URL; the
// API layer serves it and ValidContentPath lets other layers recognize it
// without duplicating the pattern.
func ContentPath(id ID) string {
	if !id.Valid() {
		return ""
	}
	return "/api/v1/uploads/" + string(id) + "/content"
}

// ValidContentPath reports whether value is exactly the shape ContentPath
// produces for some valid ID.
func ValidContentPath(value string) bool {
	return contentPathPattern.MatchString(value)
}

func newID(random io.Reader) (ID, error) {
	var raw [16]byte
	if _, err := io.ReadFull(random, raw[:]); err != nil {
		return "", err
	}
	return ID("upl_" + hex.EncodeToString(raw[:])), nil
}

func isUTC(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC
}
