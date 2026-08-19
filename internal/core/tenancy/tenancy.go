package tenancy

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	// ErrInvalidScope means an operation lacks a complete canonical tenant scope.
	ErrInvalidScope = errors.New("tenancy: invalid tenant scope")
	// ErrInvalidRecord means persisted tenant data violates domain invariants.
	ErrInvalidRecord = errors.New("tenancy: invalid persisted record")
	// ErrNotFound intentionally covers both missing and out-of-scope records.
	ErrNotFound = errors.New("tenancy: record not found")
	// ErrConflict means an optimistic version precondition did not match.
	ErrConflict = errors.New("tenancy: concurrent update")
)

// Scope is the mandatory organization/workspace authorization boundary.
// Its fields are private so callers must construct a validated scope.
type Scope struct {
	organizationID OrganizationID
	workspaceID    WorkspaceID
}

// NewScope constructs a validated tenant scope.
func NewScope(organizationID OrganizationID, workspaceID WorkspaceID) (Scope, error) {
	if !organizationID.Valid() || !workspaceID.Valid() {
		return Scope{}, ErrInvalidScope
	}
	return Scope{organizationID: organizationID, workspaceID: workspaceID}, nil
}

// ParseScope validates textual organization and workspace IDs.
func ParseScope(organizationID, workspaceID string) (Scope, error) {
	organization, err := ParseOrganizationID(organizationID)
	if err != nil {
		return Scope{}, ErrInvalidScope
	}
	workspace, err := ParseWorkspaceID(workspaceID)
	if err != nil {
		return Scope{}, ErrInvalidScope
	}
	return NewScope(organization, workspace)
}

// OrganizationID returns the scope's organization ID.
func (scope Scope) OrganizationID() OrganizationID { return scope.organizationID }

// WorkspaceID returns the scope's workspace ID.
func (scope Scope) WorkspaceID() WorkspaceID { return scope.workspaceID }

// Valid reports whether both scope components are canonical sortable IDs.
func (scope Scope) Valid() bool {
	return scope.organizationID.Valid() && scope.workspaceID.Valid()
}

// Status is the lifecycle state of a tenancy entity.
type Status string

const (
	// StatusActive permits normal use subject to authorization.
	StatusActive Status = "active"
	// StatusSuspended blocks normal use without deleting history.
	StatusSuspended Status = "suspended"
	// StatusArchived retains the entity as inactive historical state.
	StatusArchived Status = "archived"
)

// Valid reports whether the status is defined by the tenancy contract.
func (status Status) Valid() bool {
	return status == StatusActive || status == StatusSuspended || status == StatusArchived
}

// StoreKind distinguishes a sales store from a generic business unit.
type StoreKind string

const (
	// StoreKindStore represents a commerce store.
	StoreKindStore StoreKind = "store"
	// StoreKindBusinessUnit represents a non-store operational business unit.
	StoreKindBusinessUnit StoreKind = "business_unit"
)

// Valid reports whether the store kind is defined by the tenancy contract.
func (kind StoreKind) Valid() bool {
	return kind == StoreKindStore || kind == StoreKindBusinessUnit
}

// Organization is the top-level tenant boundary.
type Organization struct {
	ID        OrganizationID
	Name      string
	Status    Status
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Valid reports whether the organization satisfies persisted invariants.
func (organization Organization) Valid() bool {
	return organization.ID.Valid() && validName(organization.Name) && organization.Status.Valid() &&
		validMetadata(organization.Version, organization.CreatedAt, organization.UpdatedAt)
}

// Workspace is an authorization and operational boundary inside an organization.
type Workspace struct {
	ID             WorkspaceID
	OrganizationID OrganizationID
	Name           string
	Status         Status
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ProfileUpdate changes only tenant display names under optimistic versions.
type ProfileUpdate struct {
	OrganizationName    string
	WorkspaceName       string
	OrganizationVersion int64
	WorkspaceVersion    int64
}

// Valid reports whether the update can be safely persisted.
func (update ProfileUpdate) Valid() bool {
	return validName(update.OrganizationName) && validName(update.WorkspaceName) && update.OrganizationVersion >= 1 && update.WorkspaceVersion >= 1
}

// Valid reports whether the workspace satisfies persisted invariants.
func (workspace Workspace) Valid() bool {
	return workspace.ID.Valid() && workspace.OrganizationID.Valid() && validName(workspace.Name) &&
		workspace.Status.Valid() && validMetadata(workspace.Version, workspace.CreatedAt, workspace.UpdatedAt)
}

// Store is a store or business unit inside exactly one workspace.
type Store struct {
	ID             StoreID
	OrganizationID OrganizationID
	WorkspaceID    WorkspaceID
	Code           string
	Name           string
	Kind           StoreKind
	Status         Status
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Valid reports whether the store satisfies persisted invariants.
func (store Store) Valid() bool {
	return store.ID.Valid() && store.OrganizationID.Valid() && store.WorkspaceID.Valid() &&
		validCode(store.Code) && validName(store.Name) && store.Kind.Valid() && store.Status.Valid() &&
		validMetadata(store.Version, store.CreatedAt, store.UpdatedAt)
}

// Repository exposes only tenant-scoped lookups. Implementations must return
// ErrNotFound for both nonexistent and cross-tenant records.
type Repository interface {
	Organization(context.Context, Scope) (Organization, error)
	Workspace(context.Context, Scope) (Workspace, error)
	Store(context.Context, Scope, StoreID) (Store, error)
}

func validName(value string) bool {
	if value != strings.TrimSpace(value) || !utf8.ValidString(value) {
		return false
	}
	length := utf8.RuneCountInString(value)
	return length >= 1 && length <= 200
}

func validCode(value string) bool {
	if len(value) < 1 || len(value) > 63 {
		return false
	}
	for index, character := range []byte(value) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') ||
			(index > 0 && (character == '_' || character == '-')) {
			continue
		}
		return false
	}
	return true
}

func validMetadata(version int64, createdAt, updatedAt time.Time) bool {
	return version >= 1 && !createdAt.IsZero() && !updatedAt.IsZero() && !updatedAt.Before(createdAt)
}
