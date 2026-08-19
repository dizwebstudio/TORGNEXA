// Package privacy implements TORGNEXA's privacy classification and policy registry boundary.
package privacy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var (
	ErrInvalidPurpose   = errors.New("privacy: invalid purpose")
	ErrInvalidRetention = errors.New("privacy: invalid retention policy")
	ErrNotFound         = errors.New("privacy: not found")
	ErrConflict         = errors.New("privacy: conflict")
)

// DataClass is the canonical sensitivity classification used across storage,
// logging, event contracts, support tooling, and retention metadata.
type DataClass string

const (
	ClassPublic               DataClass = "public"
	ClassInternal             DataClass = "internal"
	ClassConfidential         DataClass = "confidential"
	ClassPersonal             DataClass = "personal"
	ClassSensitiveOperational DataClass = "sensitive_operational"
	ClassSecret               DataClass = "secret"
)

// Handling is the minimum handling requirement for a data class on a surface.
type Handling string

const (
	HandlingAllow    Handling = "allow"
	HandlingMinimize Handling = "minimize"
	HandlingRedact   Handling = "redact"
	HandlingForbid   Handling = "forbid"
)

// ClassMetadata is immutable platform metadata for one classification.
type ClassMetadata struct {
	PII       bool
	Secret    bool
	Logs      Handling
	Events    Handling
	Analytics Handling
	Support   Handling
}

var classRegistry = map[DataClass]ClassMetadata{
	ClassPublic:               {Logs: HandlingAllow, Events: HandlingAllow, Analytics: HandlingAllow, Support: HandlingAllow},
	ClassInternal:             {Logs: HandlingMinimize, Events: HandlingMinimize, Analytics: HandlingAllow, Support: HandlingMinimize},
	ClassConfidential:         {Logs: HandlingRedact, Events: HandlingMinimize, Analytics: HandlingMinimize, Support: HandlingRedact},
	ClassPersonal:             {PII: true, Logs: HandlingRedact, Events: HandlingMinimize, Analytics: HandlingMinimize, Support: HandlingRedact},
	ClassSensitiveOperational: {PII: true, Logs: HandlingRedact, Events: HandlingMinimize, Analytics: HandlingMinimize, Support: HandlingRedact},
	ClassSecret:               {Secret: true, Logs: HandlingForbid, Events: HandlingForbid, Analytics: HandlingForbid, Support: HandlingForbid},
}

func (class DataClass) Valid() bool { _, ok := classRegistry[class]; return ok }
func (class DataClass) Metadata() (ClassMetadata, bool) {
	value, ok := classRegistry[class]
	return value, ok
}
func (class DataClass) PII() bool    { value, ok := classRegistry[class]; return ok && value.PII }
func (class DataClass) Secret() bool { value, ok := classRegistry[class]; return ok && value.Secret }

// LegalBasis is deliberately provider-neutral. Jurisdiction-specific adapters
// may map these values to local statutory references without changing core code.
type LegalBasis string

const (
	BasisConsent              LegalBasis = "consent"
	BasisContract             LegalBasis = "contract"
	BasisLegalObligation      LegalBasis = "legal_obligation"
	BasisLegitimateInterest   LegalBasis = "legitimate_interest"
	BasisVitalInterest        LegalBasis = "vital_interest"
	BasisPublicTask           LegalBasis = "public_task"
	BasisOtherDocumentedBasis LegalBasis = "other_documented_basis"
)

func (basis LegalBasis) Valid() bool {
	switch basis {
	case BasisConsent, BasisContract, BasisLegalObligation, BasisLegitimateInterest, BasisVitalInterest, BasisPublicTask, BasisOtherDocumentedBasis:
		return true
	default:
		return false
	}
}

// Status is an irreversible lifecycle state for registry entries.
type Status string

const (
	StatusActive  Status = "active"
	StatusRetired Status = "retired"
)

func (status Status) Valid() bool { return status == StatusActive || status == StatusRetired }

// Disposition describes what Task 061 must eventually do when retention expires.
type Disposition string

const (
	DispositionDelete            Disposition = "delete"
	DispositionAnonymize         Disposition = "anonymize"
	DispositionArchiveThenDelete Disposition = "archive_then_delete"
	DispositionManualReview      Disposition = "manual_review"
)

func (disposition Disposition) Valid() bool {
	switch disposition {
	case DispositionDelete, DispositionAnonymize, DispositionArchiveThenDelete, DispositionManualReview:
		return true
	default:
		return false
	}
}

// Purpose records why a tenant processes a class of data and the documented
// legal/notice references that justify it.
type Purpose struct {
	OrganizationID   tenancy.OrganizationID
	WorkspaceID      tenancy.WorkspaceID
	Key              string
	Description      string
	LegalBasis       LegalBasis
	NoticeReference  string
	ConsentReference string
	AllowedClasses   []DataClass
	Status           Status
	Version          uint64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// RetentionPolicy is metadata only. Task 061 owns execution, legal holds, and
// cross-store deletion/anonymization workflows.
type RetentionPolicy struct {
	OrganizationID tenancy.OrganizationID
	WorkspaceID    tenancy.WorkspaceID
	PurposeKey     string
	DataClass      DataClass
	RetentionDays  uint32
	Disposition    Disposition
	LegalHoldOK    bool
	Status         Status
	Version        uint64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type PurposeSpec struct {
	Key              string
	Description      string
	LegalBasis       LegalBasis
	NoticeReference  string
	ConsentReference string
	AllowedClasses   []DataClass
}

type RetentionSpec struct {
	PurposeKey    string
	DataClass     DataClass
	RetentionDays uint32
	Disposition   Disposition
	LegalHoldOK   bool
}

// Repository is tenant scoped. It has no delete methods: registry rows are
// retired and remain auditable; Task 061 consumes the metadata separately.
type Repository interface {
	CreatePurpose(context.Context, tenancy.Scope, Purpose) error
	Purpose(context.Context, tenancy.Scope, string) (Purpose, error)
	UpdatePurpose(context.Context, tenancy.Scope, Purpose, uint64) error
	ActiveRetentionClasses(context.Context, tenancy.Scope, string) ([]DataClass, error)
	CreateRetention(context.Context, tenancy.Scope, RetentionPolicy) error
	Retention(context.Context, tenancy.Scope, string, DataClass) (RetentionPolicy, error)
	UpdateRetention(context.Context, tenancy.Scope, RetentionPolicy, uint64) error
}

type Clock interface{ Now() time.Time }

type Service struct {
	repository Repository
	clock      Clock
}

func NewService(repository Repository) (*Service, error) {
	return newService(repository, systemClock{})
}

func newService(repository Repository, clock Clock) (*Service, error) {
	if repository == nil || clock == nil {
		return nil, errors.New("privacy service: repository and clock are required")
	}
	return &Service{repository: repository, clock: clock}, nil
}

func (service *Service) RegisterPurpose(ctx context.Context, scope tenancy.Scope, spec PurposeSpec) (Purpose, error) {
	if err := service.validateCall(ctx, scope); err != nil {
		return Purpose{}, err
	}
	classes, err := normalizeClasses(spec.AllowedClasses)
	if err != nil {
		return Purpose{}, err
	}
	now := service.clock.Now().UTC()
	purpose := Purpose{OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Key: spec.Key, Description: spec.Description, LegalBasis: spec.LegalBasis, NoticeReference: spec.NoticeReference, ConsentReference: spec.ConsentReference, AllowedClasses: classes, Status: StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := ValidatePurpose(scope, purpose); err != nil {
		return Purpose{}, err
	}
	if err := service.repository.CreatePurpose(ctx, scope, purpose); err != nil {
		return Purpose{}, fmt.Errorf("privacy service: create purpose: %w", normalizeRepositoryError(err))
	}
	return purpose, nil
}

func (service *Service) RevisePurpose(ctx context.Context, scope tenancy.Scope, expectedVersion uint64, spec PurposeSpec) (Purpose, error) {
	if err := service.validateCall(ctx, scope); err != nil {
		return Purpose{}, err
	}
	if expectedVersion == 0 {
		return Purpose{}, ErrInvalidPurpose
	}
	current, err := service.repository.Purpose(ctx, scope, spec.Key)
	if err != nil {
		return Purpose{}, normalizeRepositoryError(err)
	}
	if current.Status != StatusActive || current.Version != expectedVersion {
		return Purpose{}, ErrConflict
	}
	classes, err := normalizeClasses(spec.AllowedClasses)
	if err != nil {
		return Purpose{}, err
	}
	activeClasses, err := service.repository.ActiveRetentionClasses(ctx, scope, spec.Key)
	if err != nil {
		return Purpose{}, normalizeRepositoryError(err)
	}
	for _, class := range activeClasses {
		if !classIn(classes, class) {
			return Purpose{}, ErrInvalidPurpose
		}
	}
	candidate := current
	candidate.Description, candidate.LegalBasis = spec.Description, spec.LegalBasis
	candidate.NoticeReference, candidate.ConsentReference = spec.NoticeReference, spec.ConsentReference
	candidate.AllowedClasses = classes
	candidate.Version++
	candidate.UpdatedAt = service.clock.Now().UTC()
	if err := ValidatePurpose(scope, candidate); err != nil {
		return Purpose{}, err
	}
	if err := service.repository.UpdatePurpose(ctx, scope, candidate, expectedVersion); err != nil {
		return Purpose{}, normalizeRepositoryError(err)
	}
	return candidate, nil
}

func (service *Service) RetirePurpose(ctx context.Context, scope tenancy.Scope, key string, expectedVersion uint64) (Purpose, error) {
	if err := service.validateCall(ctx, scope); err != nil {
		return Purpose{}, err
	}
	if !validKey(key) || expectedVersion == 0 {
		return Purpose{}, ErrInvalidPurpose
	}
	current, err := service.repository.Purpose(ctx, scope, key)
	if err != nil {
		return Purpose{}, normalizeRepositoryError(err)
	}
	if current.Status == StatusRetired {
		return current, nil
	}
	if current.Version != expectedVersion {
		return Purpose{}, ErrConflict
	}
	candidate := current
	candidate.Status = StatusRetired
	candidate.Version++
	candidate.UpdatedAt = service.clock.Now().UTC()
	if err := ValidatePurpose(scope, candidate); err != nil {
		return Purpose{}, err
	}
	if err := service.repository.UpdatePurpose(ctx, scope, candidate, expectedVersion); err != nil {
		return Purpose{}, normalizeRepositoryError(err)
	}
	return candidate, nil
}

// SetRetention creates a policy when expectedVersion is zero and revises it
// otherwise. The referenced active purpose must explicitly allow the data class.
func (service *Service) SetRetention(ctx context.Context, scope tenancy.Scope, expectedVersion uint64, spec RetentionSpec) (RetentionPolicy, error) {
	if err := service.validateCall(ctx, scope); err != nil {
		return RetentionPolicy{}, err
	}
	purpose, err := service.repository.Purpose(ctx, scope, spec.PurposeKey)
	if err != nil {
		return RetentionPolicy{}, normalizeRepositoryError(err)
	}
	if purpose.Status != StatusActive || !purposeAllows(purpose, spec.DataClass) {
		return RetentionPolicy{}, ErrInvalidRetention
	}
	now := service.clock.Now().UTC()
	if expectedVersion == 0 {
		policy := RetentionPolicy{OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), PurposeKey: spec.PurposeKey, DataClass: spec.DataClass, RetentionDays: spec.RetentionDays, Disposition: spec.Disposition, LegalHoldOK: spec.LegalHoldOK, Status: StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
		if err := ValidateRetention(scope, policy); err != nil {
			return RetentionPolicy{}, err
		}
		if err := service.repository.CreateRetention(ctx, scope, policy); err != nil {
			return RetentionPolicy{}, normalizeRepositoryError(err)
		}
		return policy, nil
	}
	current, err := service.repository.Retention(ctx, scope, spec.PurposeKey, spec.DataClass)
	if err != nil {
		return RetentionPolicy{}, normalizeRepositoryError(err)
	}
	if current.Status != StatusActive || current.Version != expectedVersion {
		return RetentionPolicy{}, ErrConflict
	}
	candidate := current
	candidate.RetentionDays, candidate.Disposition, candidate.LegalHoldOK = spec.RetentionDays, spec.Disposition, spec.LegalHoldOK
	candidate.Version++
	candidate.UpdatedAt = now
	if err := ValidateRetention(scope, candidate); err != nil {
		return RetentionPolicy{}, err
	}
	if err := service.repository.UpdateRetention(ctx, scope, candidate, expectedVersion); err != nil {
		return RetentionPolicy{}, normalizeRepositoryError(err)
	}
	return candidate, nil
}

// RetireRetention irreversibly retires one policy row without deleting its evidence.
func (service *Service) RetireRetention(ctx context.Context, scope tenancy.Scope, purposeKey string, class DataClass, expectedVersion uint64) (RetentionPolicy, error) {
	if err := service.validateCall(ctx, scope); err != nil {
		return RetentionPolicy{}, err
	}
	if expectedVersion == 0 || !class.Valid() {
		return RetentionPolicy{}, ErrInvalidRetention
	}
	current, err := service.repository.Retention(ctx, scope, purposeKey, class)
	if err != nil {
		return RetentionPolicy{}, normalizeRepositoryError(err)
	}
	if current.Status == StatusRetired {
		return current, nil
	}
	if current.Version != expectedVersion {
		return RetentionPolicy{}, ErrConflict
	}
	candidate := current
	candidate.Status = StatusRetired
	candidate.Version++
	candidate.UpdatedAt = service.clock.Now().UTC()
	if err := ValidateRetention(scope, candidate); err != nil {
		return RetentionPolicy{}, err
	}
	if err := service.repository.UpdateRetention(ctx, scope, candidate, expectedVersion); err != nil {
		return RetentionPolicy{}, normalizeRepositoryError(err)
	}
	return candidate, nil
}

func (service *Service) validateCall(ctx context.Context, scope tenancy.Scope) error {
	if ctx == nil {
		return errors.New("privacy service: context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("privacy service: %w", err)
	}
	if service == nil || service.repository == nil || service.clock == nil {
		return errors.New("privacy service: service is not initialized")
	}
	if !scope.Valid() {
		return tenancy.ErrInvalidScope
	}
	return nil
}

func ValidatePurpose(scope tenancy.Scope, purpose Purpose) error {
	if !scope.Valid() || purpose.OrganizationID != scope.OrganizationID() || purpose.WorkspaceID != scope.WorkspaceID() || !validKey(purpose.Key) || !validText(purpose.Description, 512, true) || !purpose.LegalBasis.Valid() || !validReference(purpose.NoticeReference) || !validReference(purpose.ConsentReference) || !purpose.Status.Valid() || purpose.Version == 0 || purpose.CreatedAt.IsZero() || purpose.UpdatedAt.IsZero() || purpose.UpdatedAt.Before(purpose.CreatedAt) {
		return ErrInvalidPurpose
	}
	classes, err := normalizeClasses(purpose.AllowedClasses)
	if err != nil {
		return ErrInvalidPurpose
	}
	if includesPII(classes) && purpose.NoticeReference == "" {
		return ErrInvalidPurpose
	}
	if purpose.LegalBasis == BasisConsent && purpose.ConsentReference == "" {
		return ErrInvalidPurpose
	}
	return nil
}

func ValidateRetention(scope tenancy.Scope, policy RetentionPolicy) error {
	if !scope.Valid() || policy.OrganizationID != scope.OrganizationID() || policy.WorkspaceID != scope.WorkspaceID() || !validKey(policy.PurposeKey) || !policy.DataClass.Valid() || policy.RetentionDays == 0 || policy.RetentionDays > 36500 || !policy.Disposition.Valid() || !policy.Status.Valid() || policy.Version == 0 || policy.CreatedAt.IsZero() || policy.UpdatedAt.IsZero() || policy.UpdatedAt.Before(policy.CreatedAt) {
		return ErrInvalidRetention
	}
	return nil
}

func normalizeClasses(classes []DataClass) ([]DataClass, error) {
	if len(classes) == 0 || len(classes) > len(classRegistry) {
		return nil, ErrInvalidPurpose
	}
	copyClasses := append([]DataClass(nil), classes...)
	for _, class := range copyClasses {
		if !class.Valid() {
			return nil, ErrInvalidPurpose
		}
	}
	sort.Slice(copyClasses, func(i, j int) bool { return copyClasses[i] < copyClasses[j] })
	for i := 1; i < len(copyClasses); i++ {
		if copyClasses[i] == copyClasses[i-1] {
			return nil, ErrInvalidPurpose
		}
	}
	return copyClasses, nil
}

func classIn(classes []DataClass, target DataClass) bool {
	for _, class := range classes {
		if class == target {
			return true
		}
	}
	return false
}
func includesPII(classes []DataClass) bool {
	for _, class := range classes {
		if class.PII() {
			return true
		}
	}
	return false
}
func purposeAllows(purpose Purpose, class DataClass) bool {
	for _, allowed := range purpose.AllowedClasses {
		if allowed == class {
			return true
		}
	}
	return false
}

func validKey(value string) bool {
	if len(value) < 2 || len(value) > 63 || value != strings.TrimSpace(value) {
		return false
	}
	for index, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || index > 0 && (r == '_' || r == '-' || r == '.') {
			continue
		}
		return false
	}
	return true
}
func validText(value string, limit int, required bool) bool {
	if required && value == "" {
		return false
	}
	if value != strings.TrimSpace(value) || !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func validReference(value string) bool { return value == "" || validText(value, 512, false) }
func normalizeRepositoryError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return ErrNotFound
	case errors.Is(err, ErrConflict):
		return ErrConflict
	default:
		return err
	}
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }
