package connectors

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalidAccount   = errors.New("connectors: invalid account")
	ErrAccountNotFound  = errors.New("connectors: account not found")
	ErrAccountConflict  = errors.New("connectors: account conflict")
	ErrAccountConnector = errors.New("connectors: account connector mismatch")
	ErrSecretReference  = errors.New("connectors: invalid secret reference")
)

var (
	sortableIDPattern = regexp.MustCompile(`^(?:[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}|[0-7][0-9A-HJKMNP-TV-Z]{25})$`)
	accountIDPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	secretRefPattern  = regexp.MustCompile(`^sec:v1:[0-9a-f]{32}$`)
)

type AccountStatus string

const (
	AccountDisabled  AccountStatus = "disabled"
	AccountActive    AccountStatus = "active"
	AccountSuspended AccountStatus = "suspended"
	AccountError     AccountStatus = "error"
)

func (status AccountStatus) Valid() bool {
	return status == AccountDisabled || status == AccountActive || status == AccountSuspended || status == AccountError
}

// SecretReference is an opaque stable handle. It is intentionally not an alias
// of the concrete secrets package type so provider code need only import the SDK.
type SecretReference string

func ParseSecretReference(value string) (SecretReference, error) {
	if value == "" {
		return "", nil
	}
	if !secretRefPattern.MatchString(value) {
		return "", ErrSecretReference
	}
	return SecretReference(value), nil
}

func (reference SecretReference) Valid() bool {
	return reference == "" || secretRefPattern.MatchString(string(reference))
}

// Account is the host-owned tenant binding for one connector manifest.
// Provider-specific credential values are forbidden; only SecretReference may
// cross this boundary.
type Account struct {
	ID              string          `json:"id"`
	OrganizationID  string          `json:"organization_id"`
	WorkspaceID     string          `json:"workspace_id"`
	ConnectorID     string          `json:"connector_id"`
	Family          Family          `json:"family"`
	Status          AccountStatus   `json:"status"`
	SecretReference SecretReference `json:"secret_reference,omitempty"`
	Version         int64           `json:"version"`
	Health          Health          `json:"health"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

func (account Account) Validate() error {
	bindingID := account.ConnectorID
	if !accountIDPattern.MatchString(account.ID) || !sortableIDPattern.MatchString(account.OrganizationID) || !sortableIDPattern.MatchString(account.WorkspaceID) ||
		!manifestIDPattern.MatchString(bindingID) || !account.Family.Valid() || !account.Status.Valid() || !account.SecretReference.Valid() || account.Version < 1 ||
		account.CreatedAt.IsZero() || account.UpdatedAt.IsZero() || account.CreatedAt.Location() != time.UTC || account.UpdatedAt.Location() != time.UTC || account.UpdatedAt.Before(account.CreatedAt) {
		return ErrInvalidAccount
	}
	if err := account.Health.Validate(); err != nil {
		return ErrInvalidAccount
	}
	if !account.Health.CheckedAt.IsZero() && account.Health.CheckedAt.Before(account.CreatedAt) {
		return ErrInvalidAccount
	}
	return nil
}

type AccountCreate struct {
	ID              string
	OrganizationID  string
	WorkspaceID     string
	ConnectorID     string
	SecretReference SecretReference
}

func (command AccountCreate) Validate() error {
	bindingID := command.ConnectorID
	if !accountIDPattern.MatchString(command.ID) || !sortableIDPattern.MatchString(command.OrganizationID) || !sortableIDPattern.MatchString(command.WorkspaceID) || !manifestIDPattern.MatchString(bindingID) || !command.SecretReference.Valid() {
		return ErrInvalidAccount
	}
	return nil
}

type AccountStatusChange struct {
	OrganizationID  string
	WorkspaceID     string
	AccountID       string
	Status          AccountStatus
	ExpectedVersion int64
}

func (command AccountStatusChange) Validate() error {
	if !sortableIDPattern.MatchString(command.OrganizationID) || !sortableIDPattern.MatchString(command.WorkspaceID) || !accountIDPattern.MatchString(command.AccountID) || !command.Status.Valid() || command.ExpectedVersion < 1 {
		return ErrInvalidAccount
	}
	return nil
}

type AccountHealthUpdate struct {
	OrganizationID  string
	WorkspaceID     string
	AccountID       string
	Health          Health
	ExpectedVersion int64
}

func (command AccountHealthUpdate) Validate() error {
	if !sortableIDPattern.MatchString(command.OrganizationID) || !sortableIDPattern.MatchString(command.WorkspaceID) || !accountIDPattern.MatchString(command.AccountID) || command.ExpectedVersion < 1 {
		return ErrInvalidAccount
	}
	return command.Health.Validate()
}

type AccountRepository interface {
	CreateAccount(context.Context, AccountCreate, Manifest) (Account, error)
	AccountByID(context.Context, string, string, string) (Account, error)
	ChangeAccountStatus(context.Context, AccountStatusChange) (Account, error)
	RecordAccountHealth(context.Context, AccountHealthUpdate) (Account, error)
}

func ValidateAccountAgainstManifest(account Account, manifest Manifest) error {
	if err := account.Validate(); err != nil {
		return err
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	bindingID := account.ConnectorID
	manifestID := manifest.ID
	accountFamily := account.Family
	manifestFamily := manifest.Family
	if bindingID != manifestID || accountFamily != manifestFamily {
		return ErrAccountConnector
	}
	// Disabled accounts are configuration drafts. They may exist before Task-106
	// credential enrollment, but activation always requires the manifest secret.
	if manifest.RequiresSecret() && account.Status != AccountDisabled && account.SecretReference == "" {
		return ErrSecretReference
	}
	return nil
}

func validSafeLabel(value string) bool {
	return value == strings.TrimSpace(value) && utf8.ValidString(value) && len(value) <= 200
}
