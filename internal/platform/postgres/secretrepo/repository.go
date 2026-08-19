// Package secretrepo implements tenant-scoped PostgreSQL persistence for encrypted secret references.
package secretrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const applyScopeStatement = `SELECT
  set_config('app.organization_id', $1, true),
  set_config('app.workspace_id', $2, true)`

const insertReferenceStatement = `INSERT INTO secret_references (
  reference, organization_id, workspace_id, class, status, current_version, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

const insertVersionStatement = `INSERT INTO secret_versions (
  reference, organization_id, workspace_id, version, algorithm, key_id, nonce, ciphertext, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

const activeStatement = `SELECT
  r.class, r.status, r.current_version, r.created_at, r.updated_at, r.revoked_at,
  v.version, v.algorithm, v.key_id, v.nonce, v.ciphertext, v.created_at
FROM secret_references r
JOIN secret_versions v
  ON v.reference = r.reference
 AND v.organization_id = r.organization_id
 AND v.workspace_id = r.workspace_id
 AND v.version = r.current_version
WHERE r.reference = $1`

const describeStatement = `SELECT class, status, current_version, created_at, updated_at, revoked_at
FROM secret_references WHERE reference = $1`

const lockReferenceStatement = `SELECT class, status, current_version, created_at, updated_at, revoked_at
FROM secret_references WHERE reference = $1 FOR UPDATE`

const updateVersionStatement = `UPDATE secret_references
SET current_version = $2, updated_at = $3
WHERE reference = $1 AND status = 'active' AND current_version = $4`

const revokeStatement = `UPDATE secret_references
SET status = 'revoked', revoked_at = $3, updated_at = $3
WHERE reference = $1 AND status = 'active' AND current_version = $2`

// Repository stores only opaque references, metadata, nonces, and ciphertext.
type Repository struct{ database *sql.DB }

var _ secrets.Repository = (*Repository)(nil)

func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("secret repository: database is required")
	}
	return &Repository{database: database}, nil
}

func (repository *Repository) Create(ctx context.Context, scope tenancy.Scope, metadata secrets.Metadata, version secrets.EncryptedVersion) error {
	if err := validateCall(ctx, scope, repository); err != nil {
		return err
	}
	if err := secrets.ValidateStoredPair(scope, metadata, version); err != nil {
		return err
	}
	return repository.readWrite(ctx, scope, func(tx *sql.Tx) error {
		if result, err := tx.ExecContext(ctx, insertReferenceStatement, metadata.Reference.String(), metadata.OrganizationID.String(), metadata.WorkspaceID.String(), string(metadata.Class), string(metadata.Status), metadata.CurrentVersion, metadata.CreatedAt.UTC(), metadata.UpdatedAt.UTC()); err != nil {
			return fmt.Errorf("insert secret reference: %w", err)
		} else if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
			if rowsErr != nil {
				return fmt.Errorf("insert secret reference result: %w", rowsErr)
			}
			return fmt.Errorf("insert secret reference: affected %d rows", rows)
		}
		if err := insertVersion(ctx, tx, version); err != nil {
			return err
		}
		return nil
	})
}

func (repository *Repository) Active(ctx context.Context, scope tenancy.Scope, reference secrets.Reference) (secrets.Metadata, secrets.EncryptedVersion, error) {
	if err := validateCall(ctx, scope, repository); err != nil {
		return secrets.Metadata{}, secrets.EncryptedVersion{}, err
	}
	if !reference.Valid() {
		return secrets.Metadata{}, secrets.EncryptedVersion{}, secrets.ErrInvalidReference
	}
	var metadata secrets.Metadata
	var version secrets.EncryptedVersion
	err := repository.readOnly(ctx, scope, func(tx *sql.Tx) error {
		metadata.Reference = reference
		metadata.OrganizationID = scope.OrganizationID()
		metadata.WorkspaceID = scope.WorkspaceID()
		version.Reference = reference
		version.OrganizationID = scope.OrganizationID()
		version.WorkspaceID = scope.WorkspaceID()
		var class, status string
		var revoked sql.NullTime
		if err := tx.QueryRowContext(ctx, activeStatement, reference.String()).Scan(&class, &status, &metadata.CurrentVersion, &metadata.CreatedAt, &metadata.UpdatedAt, &revoked, &version.Version, &version.Algorithm, &version.KeyID, &version.Nonce, &version.Ciphertext, &version.CreatedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return secrets.ErrNotFound
			}
			return fmt.Errorf("select active secret: %w", err)
		}
		metadata.Class = secrets.Class(class)
		metadata.Status = secrets.Status(status)
		if revoked.Valid {
			value := revoked.Time.UTC()
			metadata.RevokedAt = &value
		}
		metadata.CreatedAt = metadata.CreatedAt.UTC()
		metadata.UpdatedAt = metadata.UpdatedAt.UTC()
		version.CreatedAt = version.CreatedAt.UTC()
		return nil
	})
	if err != nil {
		return secrets.Metadata{}, secrets.EncryptedVersion{}, err
	}
	if metadata.Status == secrets.StatusRevoked {
		return secrets.Metadata{}, secrets.EncryptedVersion{}, secrets.ErrRevoked
	}
	if err := secrets.ValidateStoredPair(scope, metadata, version); err != nil {
		return secrets.Metadata{}, secrets.EncryptedVersion{}, err
	}
	return metadata, version, nil
}

func (repository *Repository) Describe(ctx context.Context, scope tenancy.Scope, reference secrets.Reference) (secrets.Metadata, error) {
	if err := validateCall(ctx, scope, repository); err != nil {
		return secrets.Metadata{}, err
	}
	if !reference.Valid() {
		return secrets.Metadata{}, secrets.ErrInvalidReference
	}
	metadata := secrets.Metadata{Reference: reference, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID()}
	err := repository.readOnly(ctx, scope, func(tx *sql.Tx) error {
		var class, status string
		var revoked sql.NullTime
		if err := tx.QueryRowContext(ctx, describeStatement, reference.String()).Scan(&class, &status, &metadata.CurrentVersion, &metadata.CreatedAt, &metadata.UpdatedAt, &revoked); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return secrets.ErrNotFound
			}
			return fmt.Errorf("describe secret: %w", err)
		}
		metadata.Class = secrets.Class(class)
		metadata.Status = secrets.Status(status)
		metadata.CreatedAt = metadata.CreatedAt.UTC()
		metadata.UpdatedAt = metadata.UpdatedAt.UTC()
		if revoked.Valid {
			value := revoked.Time.UTC()
			metadata.RevokedAt = &value
		}
		return nil
	})
	if err != nil {
		return secrets.Metadata{}, err
	}
	if err := secrets.ValidateMetadata(scope, metadata); err != nil {
		return secrets.Metadata{}, err
	}
	return metadata, nil
}

func (repository *Repository) Rotate(ctx context.Context, scope tenancy.Scope, reference secrets.Reference, expected uint64, version secrets.EncryptedVersion, now time.Time) (secrets.Metadata, error) {
	if err := validateCall(ctx, scope, repository); err != nil {
		return secrets.Metadata{}, err
	}
	if !reference.Valid() || expected == 0 || version.Version != expected+1 || now.IsZero() {
		return secrets.Metadata{}, secrets.ErrInvalidRecord
	}
	var updated secrets.Metadata
	err := repository.readWrite(ctx, scope, func(tx *sql.Tx) error {
		current, err := loadLocked(ctx, tx, scope, reference)
		if err != nil {
			return err
		}
		if current.Status != secrets.StatusActive {
			return secrets.ErrRevoked
		}
		if current.CurrentVersion != expected {
			return secrets.ErrConflict
		}
		candidate := current
		candidate.CurrentVersion = version.Version
		candidate.UpdatedAt = now.UTC()
		if err := secrets.ValidateStoredPair(scope, candidate, version); err != nil {
			return err
		}
		if err := insertVersion(ctx, tx, version); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, updateVersionStatement, reference.String(), version.Version, now.UTC(), expected)
		if err != nil {
			return fmt.Errorf("activate secret version: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("activate secret version result: %w", err)
		}
		if rows != 1 {
			return secrets.ErrConflict
		}
		updated = candidate
		return nil
	})
	if err != nil {
		return secrets.Metadata{}, err
	}
	return updated, nil
}

func (repository *Repository) Revoke(ctx context.Context, scope tenancy.Scope, reference secrets.Reference, expected uint64, now time.Time) (secrets.Metadata, error) {
	if err := validateCall(ctx, scope, repository); err != nil {
		return secrets.Metadata{}, err
	}
	if !reference.Valid() || expected == 0 || now.IsZero() {
		return secrets.Metadata{}, secrets.ErrInvalidRecord
	}
	var updated secrets.Metadata
	err := repository.readWrite(ctx, scope, func(tx *sql.Tx) error {
		current, err := loadLocked(ctx, tx, scope, reference)
		if err != nil {
			return err
		}
		if current.Status == secrets.StatusRevoked {
			updated = current
			return nil
		}
		if current.CurrentVersion != expected {
			return secrets.ErrConflict
		}
		result, err := tx.ExecContext(ctx, revokeStatement, reference.String(), expected, now.UTC())
		if err != nil {
			return fmt.Errorf("revoke secret reference: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("revoke secret result: %w", err)
		}
		if rows != 1 {
			return secrets.ErrConflict
		}
		revoked := now.UTC()
		current.Status = secrets.StatusRevoked
		current.UpdatedAt = revoked
		current.RevokedAt = &revoked
		updated = current
		return nil
	})
	if err != nil {
		return secrets.Metadata{}, err
	}
	return updated, nil
}

func loadLocked(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, reference secrets.Reference) (secrets.Metadata, error) {
	metadata := secrets.Metadata{Reference: reference, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID()}
	var class, status string
	var revoked sql.NullTime
	if err := tx.QueryRowContext(ctx, lockReferenceStatement, reference.String()).Scan(&class, &status, &metadata.CurrentVersion, &metadata.CreatedAt, &metadata.UpdatedAt, &revoked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return secrets.Metadata{}, secrets.ErrNotFound
		}
		return secrets.Metadata{}, fmt.Errorf("lock secret reference: %w", err)
	}
	metadata.Class = secrets.Class(class)
	metadata.Status = secrets.Status(status)
	metadata.CreatedAt = metadata.CreatedAt.UTC()
	metadata.UpdatedAt = metadata.UpdatedAt.UTC()
	if revoked.Valid {
		value := revoked.Time.UTC()
		metadata.RevokedAt = &value
	}
	if err := secrets.ValidateMetadata(scope, metadata); err != nil {
		return secrets.Metadata{}, err
	}
	return metadata, nil
}

func insertVersion(ctx context.Context, tx *sql.Tx, version secrets.EncryptedVersion) error {
	result, err := tx.ExecContext(ctx, insertVersionStatement, version.Reference.String(), version.OrganizationID.String(), version.WorkspaceID.String(), version.Version, version.Algorithm, version.KeyID, version.Nonce, version.Ciphertext, version.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("insert secret ciphertext version: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("insert secret ciphertext result: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("insert secret ciphertext: affected %d rows", rows)
	}
	return nil
}

func validateCall(ctx context.Context, scope tenancy.Scope, repository *Repository) error {
	if ctx == nil {
		return errors.New("secret repository: context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("secret repository: %w", err)
	}
	if repository == nil || repository.database == nil {
		return errors.New("secret repository: repository is not initialized")
	}
	if !scope.Valid() {
		return tenancy.ErrInvalidScope
	}
	return nil
}

func (repository *Repository) readWrite(ctx context.Context, scope tenancy.Scope, operation func(*sql.Tx) error) error {
	return repository.transaction(ctx, scope, false, operation)
}
func (repository *Repository) readOnly(ctx context.Context, scope tenancy.Scope, operation func(*sql.Tx) error) error {
	return repository.transaction(ctx, scope, true, operation)
}
func (repository *Repository) transaction(ctx context.Context, scope tenancy.Scope, readOnly bool, operation func(*sql.Tx) error) error {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("begin secret transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var org, workspace string
	if err := tx.QueryRowContext(ctx, applyScopeStatement, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &workspace); err != nil {
		return fmt.Errorf("apply tenant scope: %w", err)
	}
	if org != scope.OrganizationID().String() || workspace != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if err := operation(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit secret transaction: %w", err)
	}
	return nil
}
