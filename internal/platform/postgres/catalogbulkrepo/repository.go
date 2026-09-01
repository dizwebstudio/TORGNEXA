// Package catalogbulkrepo persists immutable multi-channel catalog bulk
// previews and apply evidence. It never mutates canonical PIM/Product/Offer
// records and never stores provider credentials or raw provider payloads.
package catalogbulkrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/catalogbulk"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var (
	ErrNotFound = errors.New("catalog bulk repository: not found")
	ErrConflict = errors.New("catalog bulk repository: conflict")
)

const setScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

// Repository is the tenant-scoped PostgreSQL adapter for catalog bulk evidence.
type Repository struct{ db *sql.DB }

// New constructs a catalog bulk repository.
func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("catalog bulk repository: database is required")
	}
	return &Repository{db: db}, nil
}

// SavePreview stores an immutable dry-run result.
func (r *Repository) SavePreview(ctx context.Context, scope tenancy.Scope, preview catalogbulk.Preview) (catalogbulk.Preview, error) {
	if err := r.validate(ctx, scope); err != nil || preview.Validate(preview.CreatedAt) != nil || preview.OrganizationID != scope.OrganizationID().String() || preview.WorkspaceID != scope.WorkspaceID().String() {
		return catalogbulk.Preview{}, catalogbulk.ErrInvalid
	}
	document, err := safeJSON(preview)
	if err != nil {
		return catalogbulk.Preview{}, catalogbulk.ErrInvalid
	}
	result := preview
	err = r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var existing []byte
		lookupErr := tx.QueryRowContext(ctx, `SELECT preview_document FROM catalog_bulk_previews WHERE organization_id=$1 AND workspace_id=$2 AND preview_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), preview.ID).Scan(&existing)
		if lookupErr == nil {
			if json.Unmarshal(existing, &result) != nil || result.InputDigest != preview.InputDigest {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(lookupErr, sql.ErrNoRows) {
			return fmt.Errorf("catalog bulk repository: preview lookup: %w", lookupErr)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO catalog_bulk_previews(organization_id,workspace_id,preview_id,input_digest,selection_digest,state,affected_sku_count,affected_row_count,eligible_row_count,blocked_row_count,preview_document,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12)`, scope.OrganizationID(), scope.WorkspaceID(), preview.ID, preview.InputDigest, preview.Selection.FilterDigest, preview.State, preview.AffectedSKU, preview.AffectedRows, preview.EligibleRows, preview.BlockedRows, document, preview.CreatedAt)
		if err != nil {
			return fmt.Errorf("catalog bulk repository: preview insert: %w", err)
		}
		return nil
	})
	if errors.Is(err, ErrConflict) {
		return catalogbulk.Preview{}, catalogbulk.ErrConflict
	}
	return result, err
}

// Preview loads one immutable preview in the authenticated workspace.
func (r *Repository) Preview(ctx context.Context, scope tenancy.Scope, id string) (catalogbulk.Preview, error) {
	if err := r.validate(ctx, scope); err != nil || id == "" {
		return catalogbulk.Preview{}, catalogbulk.ErrInvalid
	}
	var result catalogbulk.Preview
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var document []byte
		if err := tx.QueryRowContext(ctx, `SELECT preview_document FROM catalog_bulk_previews WHERE organization_id=$1 AND workspace_id=$2 AND preview_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id).Scan(&document); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if json.Unmarshal(document, &result) != nil || result.Validate(result.CreatedAt) != nil {
			return catalogbulk.ErrInvalid
		}
		return nil
	})
	return result, err
}

// SaveRun queues one approval-bound apply intent. The idempotency key can
// return only the original preview and can never select another operation.
func (r *Repository) SaveRun(ctx context.Context, scope tenancy.Scope, run catalogbulk.Run) (catalogbulk.Run, error) {
	if err := r.validate(ctx, scope); err != nil || run.Validate() != nil || run.OrganizationID != scope.OrganizationID().String() || run.WorkspaceID != scope.WorkspaceID().String() {
		return catalogbulk.Run{}, catalogbulk.ErrInvalid
	}
	document, err := safeJSON(run)
	if err != nil {
		return catalogbulk.Run{}, catalogbulk.ErrInvalid
	}
	result := run
	err = r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var existing []byte
		lookupErr := tx.QueryRowContext(ctx, `SELECT run_document FROM catalog_bulk_runs WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, scope.OrganizationID(), scope.WorkspaceID(), run.IdempotencyKey).Scan(&existing)
		if lookupErr == nil {
			if json.Unmarshal(existing, &result) != nil || result.PreviewID != run.PreviewID || result.InputDigest != run.InputDigest || result.Validate() != nil {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(lookupErr, sql.ErrNoRows) {
			return fmt.Errorf("catalog bulk repository: run lookup: %w", lookupErr)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO catalog_bulk_runs(organization_id,workspace_id,run_id,preview_id,idempotency_key,approval_request_id,state,input_digest,partition_count,result_count,run_document,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13)`, scope.OrganizationID(), scope.WorkspaceID(), run.ID, run.PreviewID, run.IdempotencyKey, run.ApprovalRef, run.State, run.InputDigest, len(run.Partitions), len(run.Results), document, run.CreatedAt, run.UpdatedAt)
		if err != nil {
			return fmt.Errorf("catalog bulk repository: run insert: %w", err)
		}
		return nil
	})
	if errors.Is(err, ErrConflict) {
		return catalogbulk.Run{}, catalogbulk.ErrConflict
	}
	return result, err
}

// Run loads one durable apply intent in the authenticated workspace.
func (r *Repository) Run(ctx context.Context, scope tenancy.Scope, id string) (catalogbulk.Run, error) {
	if err := r.validate(ctx, scope); err != nil || id == "" {
		return catalogbulk.Run{}, catalogbulk.ErrInvalid
	}
	var result catalogbulk.Run
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var document []byte
		if err := tx.QueryRowContext(ctx, `SELECT run_document FROM catalog_bulk_runs WHERE organization_id=$1 AND workspace_id=$2 AND run_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id).Scan(&document); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if json.Unmarshal(document, &result) != nil || result.Validate() != nil {
			return catalogbulk.ErrInvalid
		}
		return nil
	})
	return result, err
}

func safeJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(string(data))
	for _, forbidden := range []string{"authorization", "access_token", "client_secret", "private_key", "raw_provider_payload", "<script"} {
		if strings.Contains(lower, forbidden) {
			return nil, errors.New("catalog bulk repository: sensitive content")
		}
	}
	return data, nil
}

func (r *Repository) validate(ctx context.Context, scope tenancy.Scope) error {
	if ctx == nil || r == nil || r.db == nil || !scope.Valid() {
		return catalogbulk.ErrInvalid
	}
	return ctx.Err()
}

func (r *Repository) withTx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var organization, workspace string
	if err := tx.QueryRowContext(ctx, setScope, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&organization, &workspace); err != nil {
		return err
	}
	if organization != scope.OrganizationID().String() || workspace != scope.WorkspaceID().String() {
		return catalogbulk.ErrInvalid
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("catalog bulk repository: commit: %w", err)
	}
	return nil
}
