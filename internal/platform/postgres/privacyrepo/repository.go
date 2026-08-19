// Package privacyrepo implements tenant-scoped PostgreSQL persistence for privacy policy metadata.
package privacyrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/privacy"
)

const applyScopeStatement = `SELECT
  set_config('app.organization_id', $1, true),
  set_config('app.workspace_id', $2, true)`

const insertPurposeStatement = `INSERT INTO privacy_purposes (
  organization_id, workspace_id, purpose_key, description, legal_basis,
  notice_reference, consent_reference, allowed_classes, status, version, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`

const selectPurposeStatement = `SELECT description, legal_basis, notice_reference, consent_reference,
  allowed_classes, status, version, created_at, updated_at
FROM privacy_purposes WHERE purpose_key = $1`

const updatePurposeStatement = `UPDATE privacy_purposes SET
  description=$2, legal_basis=$3, notice_reference=$4, consent_reference=$5,
  allowed_classes=$6, status=$7, version=$8, updated_at=$9
WHERE purpose_key=$1 AND version=$10 AND status='active'`

const activeRetentionClassesStatement = `SELECT data_class FROM privacy_retention_policies
WHERE purpose_key=$1 AND status='active' ORDER BY data_class`

const insertRetentionStatement = `INSERT INTO privacy_retention_policies (
  organization_id, workspace_id, purpose_key, data_class, retention_days,
  disposition, legal_hold_permitted, status, version, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`

const selectRetentionStatement = `SELECT retention_days, disposition, legal_hold_permitted,
  status, version, created_at, updated_at
FROM privacy_retention_policies WHERE purpose_key=$1 AND data_class=$2`

const updateRetentionStatement = `UPDATE privacy_retention_policies SET
  retention_days=$3, disposition=$4, legal_hold_permitted=$5, status=$6,
  version=$7, updated_at=$8
WHERE purpose_key=$1 AND data_class=$2 AND version=$9 AND status='active'`

type Repository struct{ database *sql.DB }

var _ privacy.Repository = (*Repository)(nil)

func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("privacy repository: database is required")
	}
	return &Repository{database: database}, nil
}

func (repository *Repository) CreatePurpose(ctx context.Context, scope tenancy.Scope, purpose privacy.Purpose) error {
	if err := validateCall(ctx, scope, repository); err != nil {
		return err
	}
	if err := privacy.ValidatePurpose(scope, purpose); err != nil {
		return err
	}
	classes, err := json.Marshal(purpose.AllowedClasses)
	if err != nil {
		return privacy.ErrInvalidPurpose
	}
	return repository.readWrite(ctx, scope, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, insertPurposeStatement, purpose.OrganizationID.String(), purpose.WorkspaceID.String(), purpose.Key, purpose.Description, string(purpose.LegalBasis), purpose.NoticeReference, purpose.ConsentReference, classes, string(purpose.Status), purpose.Version, purpose.CreatedAt.UTC(), purpose.UpdatedAt.UTC())
		if err != nil {
			return fmt.Errorf("insert privacy purpose: %w", err)
		}
		return nil
	})
}

func (repository *Repository) Purpose(ctx context.Context, scope tenancy.Scope, key string) (privacy.Purpose, error) {
	if err := validateCall(ctx, scope, repository); err != nil {
		return privacy.Purpose{}, err
	}
	var result privacy.Purpose
	err := repository.readOnly(ctx, scope, func(tx *sql.Tx) error {
		result.OrganizationID, result.WorkspaceID, result.Key = scope.OrganizationID(), scope.WorkspaceID(), key
		var basis, status string
		var classes []byte
		if err := tx.QueryRowContext(ctx, selectPurposeStatement, key).Scan(&result.Description, &basis, &result.NoticeReference, &result.ConsentReference, &classes, &status, &result.Version, &result.CreatedAt, &result.UpdatedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return privacy.ErrNotFound
			}
			return fmt.Errorf("select privacy purpose: %w", err)
		}
		result.LegalBasis, result.Status = privacy.LegalBasis(basis), privacy.Status(status)
		if err := json.Unmarshal(classes, &result.AllowedClasses); err != nil {
			return privacy.ErrInvalidPurpose
		}
		result.CreatedAt, result.UpdatedAt = result.CreatedAt.UTC(), result.UpdatedAt.UTC()
		return privacy.ValidatePurpose(scope, result)
	})
	return result, err
}

func (repository *Repository) UpdatePurpose(ctx context.Context, scope tenancy.Scope, purpose privacy.Purpose, expected uint64) error {
	if err := validateCall(ctx, scope, repository); err != nil {
		return err
	}
	if expected == 0 || purpose.Version != expected+1 {
		return privacy.ErrInvalidPurpose
	}
	if err := privacy.ValidatePurpose(scope, purpose); err != nil {
		return err
	}
	classes, err := json.Marshal(purpose.AllowedClasses)
	if err != nil {
		return privacy.ErrInvalidPurpose
	}
	return repository.readWrite(ctx, scope, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, updatePurposeStatement, purpose.Key, purpose.Description, string(purpose.LegalBasis), purpose.NoticeReference, purpose.ConsentReference, classes, string(purpose.Status), purpose.Version, purpose.UpdatedAt.UTC(), expected)
		if err != nil {
			return fmt.Errorf("update privacy purpose: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("update privacy purpose result: %w", err)
		}
		if rows != 1 {
			return privacy.ErrConflict
		}
		return nil
	})
}

func (repository *Repository) ActiveRetentionClasses(ctx context.Context, scope tenancy.Scope, key string) ([]privacy.DataClass, error) {
	if err := validateCall(ctx, scope, repository); err != nil {
		return nil, err
	}
	var classes []privacy.DataClass
	err := repository.readOnly(ctx, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, activeRetentionClassesStatement, key)
		if err != nil {
			return fmt.Errorf("select active retention classes: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				return fmt.Errorf("scan active retention class: %w", err)
			}
			class := privacy.DataClass(raw)
			if !class.Valid() {
				return privacy.ErrInvalidRetention
			}
			classes = append(classes, class)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate active retention classes: %w", err)
		}
		return nil
	})
	return classes, err
}

func (repository *Repository) CreateRetention(ctx context.Context, scope tenancy.Scope, policy privacy.RetentionPolicy) error {
	if err := validateCall(ctx, scope, repository); err != nil {
		return err
	}
	if err := privacy.ValidateRetention(scope, policy); err != nil {
		return err
	}
	return repository.readWrite(ctx, scope, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, insertRetentionStatement, policy.OrganizationID.String(), policy.WorkspaceID.String(), policy.PurposeKey, string(policy.DataClass), policy.RetentionDays, string(policy.Disposition), policy.LegalHoldOK, string(policy.Status), policy.Version, policy.CreatedAt.UTC(), policy.UpdatedAt.UTC())
		if err != nil {
			return fmt.Errorf("insert retention policy: %w", err)
		}
		return nil
	})
}

func (repository *Repository) Retention(ctx context.Context, scope tenancy.Scope, key string, class privacy.DataClass) (privacy.RetentionPolicy, error) {
	if err := validateCall(ctx, scope, repository); err != nil {
		return privacy.RetentionPolicy{}, err
	}
	var result privacy.RetentionPolicy
	err := repository.readOnly(ctx, scope, func(tx *sql.Tx) error {
		result.OrganizationID, result.WorkspaceID, result.PurposeKey, result.DataClass = scope.OrganizationID(), scope.WorkspaceID(), key, class
		var disposition, status string
		if err := tx.QueryRowContext(ctx, selectRetentionStatement, key, string(class)).Scan(&result.RetentionDays, &disposition, &result.LegalHoldOK, &status, &result.Version, &result.CreatedAt, &result.UpdatedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return privacy.ErrNotFound
			}
			return fmt.Errorf("select retention policy: %w", err)
		}
		result.Disposition, result.Status = privacy.Disposition(disposition), privacy.Status(status)
		result.CreatedAt, result.UpdatedAt = result.CreatedAt.UTC(), result.UpdatedAt.UTC()
		return privacy.ValidateRetention(scope, result)
	})
	return result, err
}

func (repository *Repository) UpdateRetention(ctx context.Context, scope tenancy.Scope, policy privacy.RetentionPolicy, expected uint64) error {
	if err := validateCall(ctx, scope, repository); err != nil {
		return err
	}
	if expected == 0 || policy.Version != expected+1 {
		return privacy.ErrInvalidRetention
	}
	if err := privacy.ValidateRetention(scope, policy); err != nil {
		return err
	}
	return repository.readWrite(ctx, scope, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, updateRetentionStatement, policy.PurposeKey, string(policy.DataClass), policy.RetentionDays, string(policy.Disposition), policy.LegalHoldOK, string(policy.Status), policy.Version, policy.UpdatedAt.UTC(), expected)
		if err != nil {
			return fmt.Errorf("update retention policy: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("update retention policy result: %w", err)
		}
		if rows != 1 {
			return privacy.ErrConflict
		}
		return nil
	})
}

func validateCall(ctx context.Context, scope tenancy.Scope, repository *Repository) error {
	if ctx == nil {
		return errors.New("privacy repository: context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("privacy repository: %w", err)
	}
	if repository == nil || repository.database == nil {
		return errors.New("privacy repository: repository is not initialized")
	}
	if !scope.Valid() {
		return tenancy.ErrInvalidScope
	}
	return nil
}

func (repository *Repository) readOnly(ctx context.Context, scope tenancy.Scope, action func(*sql.Tx) error) error {
	return repository.transaction(ctx, scope, &sql.TxOptions{ReadOnly: true}, action)
}
func (repository *Repository) readWrite(ctx context.Context, scope tenancy.Scope, action func(*sql.Tx) error) error {
	return repository.transaction(ctx, scope, nil, action)
}
func (repository *Repository) transaction(ctx context.Context, scope tenancy.Scope, options *sql.TxOptions, action func(*sql.Tx) error) error {
	tx, err := repository.database.BeginTx(ctx, options)
	if err != nil {
		return fmt.Errorf("begin privacy transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, applyScopeStatement, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return fmt.Errorf("apply privacy tenant scope: %w", err)
	}
	if err := action(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit privacy transaction: %w", err)
	}
	return nil
}
