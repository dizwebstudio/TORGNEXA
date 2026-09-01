// Package marketplacepublicationrepo persists marketplace publication
// snapshots, operation state and append-only reconciliation evidence.
package marketplacepublicationrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/marketplacepublication"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var (
	ErrNotFound = errors.New("marketplace publication repository: not found")
	ErrConflict = errors.New("marketplace publication repository: conflict")
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

// Repository is the tenant-scoped persistence adapter for publication work.
type Repository struct{ db *sql.DB }

// New constructs a publication repository.
func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("marketplace publication repository: database is required")
	}
	return &Repository{db: db}, nil
}

// SaveSnapshot stores one immutable provider-neutral snapshot idempotently.
func (r *Repository) SaveSnapshot(ctx context.Context, scope tenancy.Scope, snapshot marketplacepublication.Snapshot) error {
	if err := r.validate(ctx, scope); err != nil || snapshot.Target.OrganizationID != scope.OrganizationID().String() || snapshot.Target.WorkspaceID != scope.WorkspaceID().String() {
		return marketplacepublication.ErrInvalid
	}
	digest, err := snapshot.ComputeDigest()
	if err != nil {
		return marketplacepublication.ErrInvalid
	}
	if snapshot.Digest != "" && snapshot.Digest != digest {
		return marketplacepublication.ErrConflict
	}
	snapshot.Digest = digest
	if snapshot.Validate() != nil {
		return marketplacepublication.ErrInvalid
	}
	document, err := json.Marshal(snapshot)
	if err != nil || strings.Contains(strings.ToLower(string(document)), "http://") || strings.Contains(strings.ToLower(string(document)), "https://") {
		return marketplacepublication.ErrInvalid
	}
	return r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT snapshot_digest FROM marketplace_publication_snapshots WHERE organization_id=$1 AND workspace_id=$2 AND snapshot_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), snapshot.ID).Scan(&existing)
		switch {
		case err == nil:
			if existing != digest {
				return ErrConflict
			}
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("marketplace publication repository: snapshot lookup: %w", err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO marketplace_publication_snapshots(organization_id,workspace_id,snapshot_id,product_id,offer_id,connector_account_id,connector_id,locale,jurisdiction,snapshot_version,snapshot_digest,snapshot_document) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb)`, scope.OrganizationID(), scope.WorkspaceID(), snapshot.ID, snapshot.Target.ProductID, snapshot.Target.OfferID, snapshot.Target.ConnectorAccountID, snapshot.Target.ConnectorID, snapshot.Target.Locale, snapshot.Target.Jurisdiction, snapshot.Version, digest, document)
		if err != nil {
			return fmt.Errorf("marketplace publication repository: insert snapshot: %w", err)
		}
		return nil
	})
}

// Enqueue creates a preflight operation or returns the exact idempotent
// operation already associated with the key.
func (r *Repository) Enqueue(ctx context.Context, scope tenancy.Scope, operation marketplacepublication.Operation) (marketplacepublication.Operation, error) {
	if err := r.validate(ctx, scope); err != nil || operation.Target.OrganizationID != scope.OrganizationID().String() || operation.Target.WorkspaceID != scope.WorkspaceID().String() || operation.Validate() != nil {
		return marketplacepublication.Operation{}, marketplacepublication.ErrInvalid
	}
	var result marketplacepublication.Operation
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT operation_id,snapshot_id,connector_account_id,connector_id,operation_kind,state,idempotency_key,remote_id,remote_operation_id,attempt,dry_run,approval_request_id,quality_receipt_id,error_code,version,created_at,updated_at FROM marketplace_publication_operations WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3 AND idempotency_key=$4`, scope.OrganizationID(), scope.WorkspaceID(), operation.Target.ConnectorAccountID, operation.IdempotencyKey).Scan(&result.ID, &result.SnapshotID, &result.Target.ConnectorAccountID, &result.Target.ConnectorID, &result.Kind, &result.State, &result.IdempotencyKey, &result.RemoteID, &result.RemoteOperationID, &result.Attempt, &result.DryRun, &result.ApprovalRef, &result.QualityReceiptRef, &result.ErrorCode, &result.Version, &result.CreatedAt, &result.UpdatedAt); err == nil {
			result.Target.OrganizationID, result.Target.WorkspaceID, result.Target.ProductID, result.Target.OfferID, result.Target.Locale, result.Target.Jurisdiction = scope.OrganizationID().String(), scope.WorkspaceID().String(), operation.Target.ProductID, operation.Target.OfferID, operation.Target.Locale, operation.Target.Jurisdiction
			result.SnapshotDigest = operation.SnapshotDigest
			if result.SnapshotID != operation.SnapshotID || result.Kind != operation.Kind || !result.Target.SameAccount(operation.Target) || result.DryRun != operation.DryRun {
				return ErrConflict
			}
			return nil
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("marketplace publication repository: operation lookup: %w", err)
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO marketplace_publication_operations(organization_id,workspace_id,operation_id,snapshot_id,connector_account_id,connector_id,operation_kind,state,idempotency_key,attempt,dry_run,approval_request_id,quality_receipt_id,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,1)`, scope.OrganizationID(), scope.WorkspaceID(), operation.ID, operation.SnapshotID, operation.Target.ConnectorAccountID, operation.Target.ConnectorID, operation.Kind, operation.State, operation.IdempotencyKey, operation.Attempt, operation.DryRun, operation.ApprovalRef, operation.QualityReceiptRef)
		if err != nil {
			return fmt.Errorf("marketplace publication repository: insert operation: %w", err)
		}
		result = operation
		return nil
	})
	if errors.Is(err, ErrConflict) {
		return marketplacepublication.Operation{}, marketplacepublication.ErrConflict
	}
	return result, err
}

// Snapshot returns the immutable publication input associated with an
// operation. The database stores the same redacted JSON accepted by the core
// validator.
func (r *Repository) Snapshot(ctx context.Context, scope tenancy.Scope, id string) (marketplacepublication.Snapshot, error) {
	if err := r.validate(ctx, scope); err != nil || id == "" || strings.ContainsAny(id, "/\x00\r\n") {
		return marketplacepublication.Snapshot{}, marketplacepublication.ErrInvalid
	}
	var snapshot marketplacepublication.Snapshot
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var document []byte
		if err := tx.QueryRowContext(ctx, `SELECT snapshot_document FROM marketplace_publication_snapshots WHERE organization_id=$1 AND workspace_id=$2 AND snapshot_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id).Scan(&document); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if json.Unmarshal(document, &snapshot) != nil || snapshot.Validate() != nil {
			return marketplacepublication.ErrInvalid
		}
		return nil
	})
	return snapshot, err
}

// ClaimQueued atomically moves one queued operation to SENDING. PostgreSQL's
// row lock makes the claim safe for multiple workers without exposing an
// unbounded cross-tenant queue to the application.
func (r *Repository) ClaimQueued(ctx context.Context, scope tenancy.Scope, now time.Time) (marketplacepublication.Operation, error) {
	if err := r.validate(ctx, scope); err != nil || now.IsZero() || now.Location() != time.UTC {
		return marketplacepublication.Operation{}, marketplacepublication.ErrInvalid
	}
	var operation marketplacepublication.Operation
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `SELECT o.operation_id,o.snapshot_id,s.product_id,s.offer_id,o.connector_account_id,o.connector_id,s.locale,s.jurisdiction,o.operation_kind,o.state,o.idempotency_key,s.snapshot_digest,o.remote_id,o.remote_operation_id,o.attempt,o.dry_run,o.approval_request_id,o.quality_receipt_id,o.error_code,o.version,o.created_at,o.updated_at FROM marketplace_publication_operations o JOIN marketplace_publication_snapshots s USING (organization_id,workspace_id,snapshot_id) WHERE o.organization_id=$1 AND o.workspace_id=$2 AND o.state='queued' ORDER BY o.updated_at,o.operation_id FOR UPDATE OF o SKIP LOCKED LIMIT 1`, scope.OrganizationID(), scope.WorkspaceID())
		if err := scanOperation(row, scope, &operation); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE marketplace_publication_operations SET state='sending',attempt=attempt+1,updated_at=$4,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND operation_id=$3 AND version=$5`, scope.OrganizationID(), scope.WorkspaceID(), operation.ID, now, operation.Version)
		if err != nil {
			return err
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO marketplace_publication_operation_events(organization_id,workspace_id,operation_id,event_id,from_state,to_state,occurred_at) VALUES($1,$2,$3,$4,'queued','sending',$5)`, scope.OrganizationID(), scope.WorkspaceID(), operation.ID, operation.ID+"/sending/"+fmt.Sprint(operation.Version+1), now)
		operation.State = marketplacepublication.StateSending
		operation.Attempt++
		operation.Version++
		operation.UpdatedAt = now
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return marketplacepublication.Operation{}, ErrNotFound
	}
	return operation, err
}

// Operation returns one tenant-scoped operation.
func (r *Repository) Operation(ctx context.Context, scope tenancy.Scope, id string) (marketplacepublication.Operation, error) {
	if err := r.validate(ctx, scope); err != nil || id == "" || strings.ContainsAny(id, "/\x00\r\n") {
		return marketplacepublication.Operation{}, marketplacepublication.ErrInvalid
	}
	var result marketplacepublication.Operation
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		err := scanOperation(tx.QueryRowContext(ctx, `SELECT o.operation_id,o.snapshot_id,s.product_id,s.offer_id,o.connector_account_id,o.connector_id,s.locale,s.jurisdiction,o.operation_kind,o.state,o.idempotency_key,s.snapshot_digest,o.remote_id,o.remote_operation_id,o.attempt,o.dry_run,o.approval_request_id,o.quality_receipt_id,o.error_code,o.version,o.created_at,o.updated_at FROM marketplace_publication_operations o JOIN marketplace_publication_snapshots s USING (organization_id,workspace_id,snapshot_id) WHERE o.organization_id=$1 AND o.workspace_id=$2 AND o.operation_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id), scope, &result)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return marketplacepublication.Operation{}, ErrNotFound
	}
	return result, err
}

// ListOperations returns recent operations for the current workspace.
func (r *Repository) ListOperations(ctx context.Context, scope tenancy.Scope, limit int) ([]marketplacepublication.Operation, error) {
	if err := r.validate(ctx, scope); err != nil || limit < 1 || limit > 100 {
		return nil, marketplacepublication.ErrInvalid
	}
	items := make([]marketplacepublication.Operation, 0, limit)
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT o.operation_id,o.snapshot_id,s.product_id,s.offer_id,o.connector_account_id,o.connector_id,s.locale,s.jurisdiction,o.operation_kind,o.state,o.idempotency_key,s.snapshot_digest,o.remote_id,o.remote_operation_id,o.attempt,o.dry_run,o.approval_request_id,o.quality_receipt_id,o.error_code,o.version,o.created_at,o.updated_at FROM marketplace_publication_operations o JOIN marketplace_publication_snapshots s USING (organization_id,workspace_id,snapshot_id) WHERE o.organization_id=$1 AND o.workspace_id=$2 ORDER BY o.updated_at DESC,o.operation_id DESC LIMIT $3`, scope.OrganizationID(), scope.WorkspaceID(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item marketplacepublication.Operation
			if err := scanOperation(rows, scope, &item); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// ListPending returns operations whose remote result still needs a bounded
// status read. It is intentionally a read-only projection; the subsequent
// compare-and-swap transition remains the authority for local state.
func (r *Repository) ListPending(ctx context.Context, scope tenancy.Scope, limit int) ([]marketplacepublication.Operation, error) {
	if err := r.validate(ctx, scope); err != nil || limit < 1 || limit > 100 {
		return nil, marketplacepublication.ErrInvalid
	}
	items := make([]marketplacepublication.Operation, 0, limit)
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT o.operation_id,o.snapshot_id,s.product_id,s.offer_id,o.connector_account_id,o.connector_id,s.locale,s.jurisdiction,o.operation_kind,o.state,o.idempotency_key,s.snapshot_digest,o.remote_id,o.remote_operation_id,o.attempt,o.dry_run,o.approval_request_id,o.quality_receipt_id,o.error_code,o.version,o.created_at,o.updated_at FROM marketplace_publication_operations o JOIN marketplace_publication_snapshots s USING (organization_id,workspace_id,snapshot_id) WHERE o.organization_id=$1 AND o.workspace_id=$2 AND o.state IN ('accepted','processing','unknown') ORDER BY o.updated_at,o.operation_id LIMIT $3`, scope.OrganizationID(), scope.WorkspaceID(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item marketplacepublication.Operation
			if err := scanOperation(rows, scope, &item); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// UpdateState records an idempotent worker transition and its append-only
// event. The expected version prevents concurrent workers from overwriting one
// another.
func (r *Repository) UpdateState(ctx context.Context, scope tenancy.Scope, operation marketplacepublication.Operation, expectedVersion int64) error {
	if err := r.validate(ctx, scope); err != nil || operation.Validate() != nil || expectedVersion < 1 || operation.Target.OrganizationID != scope.OrganizationID().String() || operation.Target.WorkspaceID != scope.WorkspaceID().String() {
		return marketplacepublication.ErrInvalid
	}
	return r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var from marketplacepublication.State
		if err := tx.QueryRowContext(ctx, `SELECT state FROM marketplace_publication_operations WHERE organization_id=$1 AND workspace_id=$2 AND operation_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), operation.ID).Scan(&from); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if !marketplacepublication.CanTransition(from, operation.State) {
			return marketplacepublication.ErrInvalidState
		}
		result, err := tx.ExecContext(ctx, `UPDATE marketplace_publication_operations SET state=$4,remote_id=$5,remote_operation_id=$6,attempt=$7,error_code=$8,updated_at=$9,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND operation_id=$3 AND version=$10`, scope.OrganizationID(), scope.WorkspaceID(), operation.ID, operation.State, operation.RemoteID, operation.RemoteOperationID, operation.Attempt, operation.ErrorCode, operation.UpdatedAt.UTC(), expectedVersion)
		if err != nil {
			return err
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO marketplace_publication_operation_events(organization_id,workspace_id,operation_id,event_id,from_state,to_state,error_code,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, scope.OrganizationID(), scope.WorkspaceID(), operation.ID, operation.ID+"/"+fmt.Sprint(expectedVersion+1), from, operation.State, operation.ErrorCode, operation.UpdatedAt.UTC())
		return err
	})
}

// RecordObservation appends a normalized remote observation.
func (r *Repository) RecordObservation(ctx context.Context, scope tenancy.Scope, observation marketplacepublication.RemoteObservation, operationID, observationID string) error {
	if err := r.validate(ctx, scope); err != nil || observation.Validate() != nil || operationID == "" || observationID == "" {
		return marketplacepublication.ErrInvalid
	}
	return r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO marketplace_publication_observations(organization_id,workspace_id,observation_id,operation_id,remote_id,remote_operation_id,state,moderation,snapshot_digest,observed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`, scope.OrganizationID(), scope.WorkspaceID(), observationID, operationID, observation.RemoteID, observation.RemoteOperationID, observation.State, observation.Moderation, observation.SnapshotDigest, observation.ObservedAt.UTC())
		return err
	})
}

// RecordDrift appends a redacted reconciliation drift.
func (r *Repository) RecordDrift(ctx context.Context, scope tenancy.Scope, drift marketplacepublication.Drift, operationID, driftID string) error {
	if err := r.validate(ctx, scope); err != nil || drift.Validate() != nil || operationID == "" || driftID == "" {
		return marketplacepublication.ErrInvalid
	}
	return r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO marketplace_publication_drifts(organization_id,workspace_id,drift_id,operation_id,snapshot_id,drift_type,remote_id,expected_digest,observed_digest,observed_state,detected_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`, scope.OrganizationID(), scope.WorkspaceID(), driftID, operationID, drift.SnapshotID, drift.Type, drift.RemoteID, drift.ExpectedDigest, drift.ObservedDigest, drift.ObservedState, drift.DetectedAt.UTC())
		return err
	})
}

// ListDrifts returns redacted reconciliation findings for one operation.
func (r *Repository) ListDrifts(ctx context.Context, scope tenancy.Scope, operationID string, limit int) ([]marketplacepublication.Drift, error) {
	if err := r.validate(ctx, scope); err != nil || operationID == "" || strings.ContainsAny(operationID, "/\x00\r\n") || limit < 1 || limit > 100 {
		return nil, marketplacepublication.ErrInvalid
	}
	items := make([]marketplacepublication.Drift, 0, limit)
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT drift_type,snapshot_id,remote_id,expected_digest,observed_digest,observed_state,detected_at FROM marketplace_publication_drifts WHERE organization_id=$1 AND workspace_id=$2 AND operation_id=$3 ORDER BY detected_at DESC,drift_id DESC LIMIT $4`, scope.OrganizationID(), scope.WorkspaceID(), operationID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item marketplacepublication.Drift
			if err := rows.Scan(&item.Type, &item.SnapshotID, &item.RemoteID, &item.ExpectedDigest, &item.ObservedDigest, &item.ObservedState, &item.DetectedAt); err != nil {
				return err
			}
			item.DetectedAt = item.DetectedAt.UTC()
			if item.Validate() != nil {
				return marketplacepublication.ErrInvalid
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

func scanOperation(row interface{ Scan(...any) error }, scope tenancy.Scope, operation *marketplacepublication.Operation) error {
	if err := row.Scan(&operation.ID, &operation.SnapshotID, &operation.Target.ProductID, &operation.Target.OfferID, &operation.Target.ConnectorAccountID, &operation.Target.ConnectorID, &operation.Target.Locale, &operation.Target.Jurisdiction, &operation.Kind, &operation.State, &operation.IdempotencyKey, &operation.SnapshotDigest, &operation.RemoteID, &operation.RemoteOperationID, &operation.Attempt, &operation.DryRun, &operation.ApprovalRef, &operation.QualityReceiptRef, &operation.ErrorCode, &operation.Version, &operation.CreatedAt, &operation.UpdatedAt); err != nil {
		return err
	}
	operation.Target.OrganizationID, operation.Target.WorkspaceID = scope.OrganizationID().String(), scope.WorkspaceID().String()
	operation.CreatedAt, operation.UpdatedAt = operation.CreatedAt.UTC(), operation.UpdatedAt.UTC()
	return operation.Validate()
}

func (r *Repository) validate(ctx context.Context, scope tenancy.Scope) error {
	if ctx == nil || r == nil || r.db == nil || !scope.Valid() {
		return marketplacepublication.ErrInvalid
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
		return marketplacepublication.ErrInvalid
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("marketplace publication repository: commit: %w", err)
	}
	return nil
}
