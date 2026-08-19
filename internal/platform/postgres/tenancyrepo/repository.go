// Package tenancyrepo implements tenant-scoped PostgreSQL repository ports.
package tenancyrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const applyScopeStatement = `SELECT
  set_config('app.organization_id', $1, true),
  set_config('app.workspace_id', $2, true)`

const organizationQuery = `SELECT
  o.id, o.name, o.status, o.version, o.created_at, o.updated_at
FROM organizations AS o
JOIN workspaces AS w
  ON w.organization_id = o.id
WHERE o.id = $1
  AND w.organization_id = $1
  AND w.id = $2`

const workspaceQuery = `SELECT
  w.id, w.organization_id, w.name, w.status, w.version, w.created_at, w.updated_at
FROM workspaces AS w
WHERE w.organization_id = $1
  AND w.id = $2`

const storeQuery = `SELECT
  s.id, s.organization_id, s.workspace_id, s.code, s.name, s.kind,
  s.status, s.version, s.created_at, s.updated_at
FROM stores AS s
WHERE s.organization_id = $1
  AND s.workspace_id = $2
  AND s.id = $3`

const updateProfileQuery = `WITH updated_organization AS (
  UPDATE organizations SET name=$3, version=version+1, updated_at=clock_timestamp()
  WHERE id=$1 AND version=$4
  RETURNING id,name,status,version,created_at,updated_at
), updated_workspace AS (
  UPDATE workspaces SET name=$5, version=version+1, updated_at=clock_timestamp()
  WHERE organization_id=$1 AND id=$2 AND version=$6 AND EXISTS (SELECT 1 FROM updated_organization)
  RETURNING id,organization_id,name,status,version,created_at,updated_at
)
SELECT o.id,o.name,o.status,o.version,o.created_at,o.updated_at,
       w.id,w.organization_id,w.name,w.status,w.version,w.created_at,w.updated_at
FROM updated_organization o CROSS JOIN updated_workspace w`

// Repository provides PostgreSQL-backed tenant hierarchy lookups.
type Repository struct {
	transactions transactor
}

var _ tenancy.Repository = (*Repository)(nil)

// New constructs a tenant repository over a database/sql connection pool.
// The pool must use an application role subject to the migration's forced RLS
// policies; privileged migration/repair roles must not be passed here.
func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("tenancy repository: database is required")
	}
	return newRepository(sqlTransactor{database: database}), nil
}

func newRepository(transactions transactor) *Repository {
	return &Repository{transactions: transactions}
}

// Organization returns the organization containing the scoped workspace.
func (repository *Repository) Organization(ctx context.Context, scope tenancy.Scope) (tenancy.Organization, error) {
	var organization tenancy.Organization
	err := repository.withScope(ctx, scope, func(queries queryer) error {
		row := queries.QueryRowContext(ctx, organizationQuery, scope.OrganizationID().String(), scope.WorkspaceID().String())
		value, err := scanOrganization(row)
		if err != nil {
			return lookupError("organization", err)
		}
		if value.ID != scope.OrganizationID() {
			return fmt.Errorf("organization lookup: %w", tenancy.ErrInvalidRecord)
		}
		organization = value
		return nil
	})
	return organization, err
}

// Workspace returns exactly the workspace named by the tenant scope.
func (repository *Repository) Workspace(ctx context.Context, scope tenancy.Scope) (tenancy.Workspace, error) {
	var workspace tenancy.Workspace
	err := repository.withScope(ctx, scope, func(queries queryer) error {
		row := queries.QueryRowContext(ctx, workspaceQuery, scope.OrganizationID().String(), scope.WorkspaceID().String())
		value, err := scanWorkspace(row)
		if err != nil {
			return lookupError("workspace", err)
		}
		if value.OrganizationID != scope.OrganizationID() || value.ID != scope.WorkspaceID() {
			return fmt.Errorf("workspace lookup: %w", tenancy.ErrInvalidRecord)
		}
		workspace = value
		return nil
	})
	return workspace, err
}

// Store returns a store only when its organization and workspace both match
// the mandatory tenant scope.
func (repository *Repository) Store(ctx context.Context, scope tenancy.Scope, storeID tenancy.StoreID) (tenancy.Store, error) {
	if !storeID.Valid() {
		return tenancy.Store{}, tenancy.ErrInvalidID
	}
	var store tenancy.Store
	err := repository.withScope(ctx, scope, func(queries queryer) error {
		row := queries.QueryRowContext(ctx, storeQuery, scope.OrganizationID().String(), scope.WorkspaceID().String(), storeID.String())
		value, err := scanStore(row)
		if err != nil {
			return lookupError("store", err)
		}
		if value.OrganizationID != scope.OrganizationID() || value.WorkspaceID != scope.WorkspaceID() || value.ID != storeID {
			return fmt.Errorf("store lookup: %w", tenancy.ErrInvalidRecord)
		}
		store = value
		return nil
	})
	return store, err
}

// UpdateProfile atomically changes organization and workspace display names.
func (repository *Repository) UpdateProfile(ctx context.Context, scope tenancy.Scope, update tenancy.ProfileUpdate) (tenancy.Organization, tenancy.Workspace, error) {
	if !update.Valid() {
		return tenancy.Organization{}, tenancy.Workspace{}, tenancy.ErrInvalidRecord
	}
	var organization tenancy.Organization
	var workspace tenancy.Workspace
	err := repository.withWriteScope(ctx, scope, func(queries queryer) error {
		row := queries.QueryRowContext(ctx, updateProfileQuery, scope.OrganizationID().String(), scope.WorkspaceID().String(), update.OrganizationName, update.OrganizationVersion, update.WorkspaceName, update.WorkspaceVersion)
		var organizationID, organizationStatus, workspaceID, workspaceOrganizationID, workspaceStatus string
		if err := row.Scan(&organizationID, &organization.Name, &organizationStatus, &organization.Version, &organization.CreatedAt, &organization.UpdatedAt, &workspaceID, &workspaceOrganizationID, &workspace.Name, &workspaceStatus, &workspace.Version, &workspace.CreatedAt, &workspace.UpdatedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return tenancy.ErrConflict
			}
			return err
		}
		organization.ID, _ = tenancy.ParseOrganizationID(organizationID)
		organization.Status = tenancy.Status(organizationStatus)
		workspace.ID, _ = tenancy.ParseWorkspaceID(workspaceID)
		workspace.OrganizationID, _ = tenancy.ParseOrganizationID(workspaceOrganizationID)
		workspace.Status = tenancy.Status(workspaceStatus)
		organization.CreatedAt, organization.UpdatedAt = organization.CreatedAt.UTC(), organization.UpdatedAt.UTC()
		workspace.CreatedAt, workspace.UpdatedAt = workspace.CreatedAt.UTC(), workspace.UpdatedAt.UTC()
		if !organization.Valid() || !workspace.Valid() || organization.ID != scope.OrganizationID() || workspace.ID != scope.WorkspaceID() {
			return tenancy.ErrInvalidRecord
		}
		return nil
	})
	return organization, workspace, err
}

func (repository *Repository) withScope(ctx context.Context, scope tenancy.Scope, operation func(queryer) error) error {
	return repository.withScopedTransaction(ctx, scope, true, operation)
}

func (repository *Repository) withWriteScope(ctx context.Context, scope tenancy.Scope, operation func(queryer) error) error {
	return repository.withScopedTransaction(ctx, scope, false, operation)
}

func (repository *Repository) withScopedTransaction(ctx context.Context, scope tenancy.Scope, readOnly bool, operation func(queryer) error) error {
	if ctx == nil {
		return errors.New("tenancy repository: context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("tenancy repository: %w", err)
	}
	if !scope.Valid() {
		return tenancy.ErrInvalidScope
	}
	if repository == nil || repository.transactions == nil {
		return errors.New("tenancy repository: repository is not initialized")
	}
	return repository.transactions.run(ctx, readOnly, func(queries queryer) error {
		var appliedOrganization, appliedWorkspace string
		if err := queries.QueryRowContext(
			ctx,
			applyScopeStatement,
			scope.OrganizationID().String(),
			scope.WorkspaceID().String(),
		).Scan(&appliedOrganization, &appliedWorkspace); err != nil {
			return fmt.Errorf("apply tenant scope: %w", err)
		}
		if appliedOrganization != scope.OrganizationID().String() || appliedWorkspace != scope.WorkspaceID().String() {
			return fmt.Errorf("apply tenant scope: %w", tenancy.ErrInvalidScope)
		}
		return operation(queries)
	})
}

func scanOrganization(row rowScanner) (tenancy.Organization, error) {
	var id, status string
	var organization tenancy.Organization
	if err := row.Scan(&id, &organization.Name, &status, &organization.Version, &organization.CreatedAt, &organization.UpdatedAt); err != nil {
		return tenancy.Organization{}, err
	}
	organizationID, err := tenancy.ParseOrganizationID(id)
	if err != nil {
		return tenancy.Organization{}, tenancy.ErrInvalidRecord
	}
	organization.ID = organizationID
	organization.Status = tenancy.Status(status)
	organization.CreatedAt = organization.CreatedAt.UTC()
	organization.UpdatedAt = organization.UpdatedAt.UTC()
	if !organization.Valid() {
		return tenancy.Organization{}, tenancy.ErrInvalidRecord
	}
	return organization, nil
}

func scanWorkspace(row rowScanner) (tenancy.Workspace, error) {
	var id, organizationID, status string
	var workspace tenancy.Workspace
	if err := row.Scan(&id, &organizationID, &workspace.Name, &status, &workspace.Version, &workspace.CreatedAt, &workspace.UpdatedAt); err != nil {
		return tenancy.Workspace{}, err
	}
	parsedID, err := tenancy.ParseWorkspaceID(id)
	if err != nil {
		return tenancy.Workspace{}, tenancy.ErrInvalidRecord
	}
	parsedOrganizationID, err := tenancy.ParseOrganizationID(organizationID)
	if err != nil {
		return tenancy.Workspace{}, tenancy.ErrInvalidRecord
	}
	workspace.ID = parsedID
	workspace.OrganizationID = parsedOrganizationID
	workspace.Status = tenancy.Status(status)
	workspace.CreatedAt = workspace.CreatedAt.UTC()
	workspace.UpdatedAt = workspace.UpdatedAt.UTC()
	if !workspace.Valid() {
		return tenancy.Workspace{}, tenancy.ErrInvalidRecord
	}
	return workspace, nil
}

func scanStore(row rowScanner) (tenancy.Store, error) {
	var id, organizationID, workspaceID, kind, status string
	var store tenancy.Store
	if err := row.Scan(
		&id,
		&organizationID,
		&workspaceID,
		&store.Code,
		&store.Name,
		&kind,
		&status,
		&store.Version,
		&store.CreatedAt,
		&store.UpdatedAt,
	); err != nil {
		return tenancy.Store{}, err
	}
	parsedID, err := tenancy.ParseStoreID(id)
	if err != nil {
		return tenancy.Store{}, tenancy.ErrInvalidRecord
	}
	parsedOrganizationID, err := tenancy.ParseOrganizationID(organizationID)
	if err != nil {
		return tenancy.Store{}, tenancy.ErrInvalidRecord
	}
	parsedWorkspaceID, err := tenancy.ParseWorkspaceID(workspaceID)
	if err != nil {
		return tenancy.Store{}, tenancy.ErrInvalidRecord
	}
	store.ID = parsedID
	store.OrganizationID = parsedOrganizationID
	store.WorkspaceID = parsedWorkspaceID
	store.Kind = tenancy.StoreKind(kind)
	store.Status = tenancy.Status(status)
	store.CreatedAt = store.CreatedAt.UTC()
	store.UpdatedAt = store.UpdatedAt.UTC()
	if !store.Valid() {
		return tenancy.Store{}, tenancy.ErrInvalidRecord
	}
	return store, nil
}

func lookupError(entity string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s lookup: %w", entity, tenancy.ErrNotFound)
	}
	if errors.Is(err, tenancy.ErrInvalidRecord) {
		return fmt.Errorf("%s lookup: %w", entity, tenancy.ErrInvalidRecord)
	}
	return fmt.Errorf("%s lookup failed: %w", entity, err)
}

type rowScanner interface {
	Scan(...any) error
}

type rowsScanner interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) rowScanner
	QueryContext(context.Context, string, ...any) (rowsScanner, error)
}

type transactor interface {
	run(context.Context, bool, func(queryer) error) error
}

type sqlTransactor struct {
	database *sql.DB
}

func (transactions sqlTransactor) run(ctx context.Context, readOnly bool, operation func(queryer) error) error {
	transaction, err := transactions.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("begin tenant read transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback()
	}()
	queries := sqlQueries{transaction: transaction}
	if err := operation(queries); err != nil {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return errors.Join(err, fmt.Errorf("rollback tenant read transaction: %w", rollbackErr))
		}
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit tenant read transaction: %w", err)
	}
	return nil
}

type sqlQueries struct {
	transaction *sql.Tx
}

func (queries sqlQueries) QueryRowContext(ctx context.Context, statement string, arguments ...any) rowScanner {
	return queries.transaction.QueryRowContext(ctx, statement, arguments...)
}

func (queries sqlQueries) QueryContext(ctx context.Context, statement string, arguments ...any) (rowsScanner, error) {
	return queries.transaction.QueryContext(ctx, statement, arguments...)
}
