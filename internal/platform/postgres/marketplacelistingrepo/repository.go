// Package marketplacelistingrepo persists marketplace taxonomy and batch
// qualification evidence. Provider payloads and credentials are deliberately
// outside this repository.
package marketplacelistingrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/marketplacelisting"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var (
	ErrNotFound = errors.New("marketplace listing repository: not found")
	ErrConflict = errors.New("marketplace listing repository: conflict")
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

// Repository is a tenant-scoped PostgreSQL adapter for listing workspace data.
type Repository struct{ db *sql.DB }

// New constructs a marketplace listing repository.
func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("marketplace listing repository: database is required")
	}
	return &Repository{db: db}, nil
}

// SaveTaxonomy stores one immutable, versioned taxonomy document.
func (r *Repository) SaveTaxonomy(ctx context.Context, scope tenancy.Scope, taxonomy marketplacelisting.Taxonomy) error {
	if err := r.validate(ctx, scope); err != nil || taxonomy.Validate() != nil {
		return marketplacelisting.ErrInvalid
	}
	fingerprint, err := taxonomy.ComputeFingerprint()
	if err != nil {
		return marketplacelisting.ErrInvalid
	}
	if taxonomy.Fingerprint != "" && taxonomy.Fingerprint != fingerprint {
		return marketplacelisting.ErrConflict
	}
	taxonomy.Fingerprint = fingerprint
	document, err := json.Marshal(taxonomy)
	if err != nil || strings.Contains(strings.ToLower(string(document)), "http://") || strings.Contains(strings.ToLower(string(document)), "https://") {
		return marketplacelisting.ErrInvalid
	}
	return r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT fingerprint FROM marketplace_listing_taxonomies WHERE organization_id=$1 AND workspace_id=$2 AND taxonomy_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), taxonomy.ID).Scan(&existing)
		if err == nil {
			if existing != fingerprint {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("marketplace listing repository: taxonomy lookup: %w", err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO marketplace_listing_taxonomies(organization_id,workspace_id,taxonomy_id,connector_id,locale,jurisdiction,taxonomy_version,source,fingerprint,observed_at,fresh_until,taxonomy_document) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb)`, scope.OrganizationID(), scope.WorkspaceID(), taxonomy.ID, taxonomy.ChannelID, taxonomy.Locale, taxonomy.Jurisdiction, taxonomy.Version, taxonomy.Source, fingerprint, taxonomy.ObservedAt, taxonomy.FreshUntil, document)
		if err != nil {
			return fmt.Errorf("marketplace listing repository: taxonomy insert: %w", err)
		}
		return nil
	})
}

// Taxonomy loads one taxonomy document without exposing provider payloads.
func (r *Repository) Taxonomy(ctx context.Context, scope tenancy.Scope, id string) (marketplacelisting.Taxonomy, error) {
	if err := r.validate(ctx, scope); err != nil || id == "" {
		return marketplacelisting.Taxonomy{}, marketplacelisting.ErrInvalid
	}
	var result marketplacelisting.Taxonomy
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var document []byte
		if err := tx.QueryRowContext(ctx, `SELECT taxonomy_document FROM marketplace_listing_taxonomies WHERE organization_id=$1 AND workspace_id=$2 AND taxonomy_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id).Scan(&document); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if json.Unmarshal(document, &result) != nil || result.Validate() != nil {
			return marketplacelisting.ErrInvalid
		}
		return nil
	})
	return result, err
}

// SaveBatch records a preview-backed apply request. The idempotency key
// returns the original run and can never silently select another preview.
func (r *Repository) SaveBatch(ctx context.Context, scope tenancy.Scope, run marketplacelisting.BatchRun) (marketplacelisting.BatchRun, error) {
	if err := r.validate(ctx, scope); err != nil || run.Validate() != nil || run.OrganizationID != scope.OrganizationID().String() || run.WorkspaceID != scope.WorkspaceID().String() {
		return marketplacelisting.BatchRun{}, marketplacelisting.ErrInvalid
	}
	document, err := json.Marshal(run)
	if err != nil || strings.Contains(strings.ToLower(string(document)), "http://") || strings.Contains(strings.ToLower(string(document)), "https://") {
		return marketplacelisting.BatchRun{}, marketplacelisting.ErrInvalid
	}
	result := run
	err = r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var existingDocument []byte
		err := tx.QueryRowContext(ctx, `SELECT batch_document FROM marketplace_listing_batches WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, scope.OrganizationID(), scope.WorkspaceID(), run.IdempotencyKey).Scan(&existingDocument)
		if err == nil {
			if json.Unmarshal(existingDocument, &result) != nil || result.PreviewID != run.PreviewID || result.InputDigest != run.InputDigest || result.ApprovalRef != run.ApprovalRef || !reflect.DeepEqual(result.Rows, run.Rows) || !equalStrings(result.RemoteOperationIDs, run.RemoteOperationIDs) || result.Validate() != nil {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("marketplace listing repository: batch lookup: %w", err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO marketplace_listing_batches(organization_id,workspace_id,batch_id,preview_id,idempotency_key,approval_request_id,state,input_digest,affected_count,eligible_count,blocked_count,batch_document,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, scope.OrganizationID(), scope.WorkspaceID(), run.ID, run.PreviewID, run.IdempotencyKey, run.ApprovalRef, run.State, run.InputDigest, len(run.Rows), countEligible(run.Rows), len(run.Rows)-countEligible(run.Rows), document, run.CreatedAt, run.UpdatedAt)
		if err != nil {
			return fmt.Errorf("marketplace listing repository: batch insert: %w", err)
		}
		return nil
	})
	if errors.Is(err, ErrConflict) {
		return marketplacelisting.BatchRun{}, marketplacelisting.ErrConflict
	}
	return result, err
}

// Batch loads one durable batch apply run.
func (r *Repository) Batch(ctx context.Context, scope tenancy.Scope, id string) (marketplacelisting.BatchRun, error) {
	if err := r.validate(ctx, scope); err != nil || id == "" {
		return marketplacelisting.BatchRun{}, marketplacelisting.ErrInvalid
	}
	var result marketplacelisting.BatchRun
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var document []byte
		if err := tx.QueryRowContext(ctx, `SELECT batch_document FROM marketplace_listing_batches WHERE organization_id=$1 AND workspace_id=$2 AND batch_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id).Scan(&document); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if json.Unmarshal(document, &result) != nil || result.Validate() != nil {
			return marketplacelisting.ErrInvalid
		}
		return nil
	})
	return result, err
}

func countEligible(rows []marketplacelisting.BatchRow) int {
	count := 0
	for _, row := range rows {
		if row.Eligible {
			count++
		}
	}
	return count
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (r *Repository) validate(ctx context.Context, scope tenancy.Scope) error {
	if ctx == nil || r == nil || r.db == nil || !scope.Valid() {
		return marketplacelisting.ErrInvalid
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
	if err := tx.QueryRowContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&organization, &workspace); err != nil {
		return err
	}
	if organization != scope.OrganizationID().String() || workspace != scope.WorkspaceID().String() {
		return marketplacelisting.ErrInvalid
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("marketplace listing repository: commit: %w", err)
	}
	return nil
}
