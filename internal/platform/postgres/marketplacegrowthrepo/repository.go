// Package marketplacegrowthrepo persists the sanitized Task 225 evidence and
// approved promotion/advertising intents. PostgreSQL remains authoritative;
// remote responses are reduced to typed observations before this boundary.
package marketplacegrowthrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/marketplacegrowth"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var (
	ErrNotFound = errors.New("marketplace growth repository: not found")
	ErrConflict = errors.New("marketplace growth repository: conflict")
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

// Repository is a tenant-scoped PostgreSQL adapter for Task 225.
type Repository struct{ db *sql.DB }

// New constructs a repository over the application database.
func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("marketplace growth repository: database is required")
	}
	return &Repository{db: db}, nil
}

// SavePreview stores immutable preview evidence. Replaying the same preview
// digest is harmless; a different document under the same identifier conflicts.
func (r *Repository) SavePreview(ctx context.Context, scope tenancy.Scope, preview marketplacegrowth.Preview) error {
	if err := r.validate(ctx, scope); err != nil || preview.Validate() != nil {
		return marketplacegrowth.ErrInvalid
	}
	document, err := json.Marshal(preview)
	if err != nil || forbiddenDocument(document) {
		return marketplacegrowth.ErrInvalid
	}
	return r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var existing []byte
		err := tx.QueryRowContext(ctx, `SELECT preview_document FROM marketplace_growth_previews WHERE organization_id=$1 AND workspace_id=$2 AND preview_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), preview.ID).Scan(&existing)
		if err == nil {
			if string(existing) != string(document) {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("marketplace growth repository: preview lookup: %w", err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO marketplace_growth_previews(organization_id,workspace_id,preview_id,operation,channel_id,account_id,target_id,input_digest,rule_version,state,affected_count,eligible_count,blocked_count,preview_document,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,$15)`, scope.OrganizationID(), scope.WorkspaceID(), preview.ID, preview.Operation, preview.ChannelID, preview.AccountID, preview.TargetID, preview.InputDigest, preview.RuleVersion, preview.State, preview.AffectedCount, preview.EligibleCount, preview.BlockedCount, document, preview.CreatedAt)
		return err
	})
}

// Preview loads one immutable preview in the current tenant scope.
func (r *Repository) Preview(ctx context.Context, scope tenancy.Scope, id string) (marketplacegrowth.Preview, error) {
	if err := r.validate(ctx, scope); err != nil || !validID(id) {
		return marketplacegrowth.Preview{}, marketplacegrowth.ErrInvalid
	}
	var result marketplacegrowth.Preview
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var document []byte
		if err := tx.QueryRowContext(ctx, `SELECT preview_document FROM marketplace_growth_previews WHERE organization_id=$1 AND workspace_id=$2 AND preview_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id).Scan(&document); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if json.Unmarshal(document, &result) != nil || result.Validate() != nil {
			return marketplacegrowth.ErrInvalid
		}
		return nil
	})
	return result, err
}

// SaveOperation stores an approved intent with retry-safe idempotency. The
// initial state is qualification_required until a qualified worker is wired.
func (r *Repository) SaveOperation(ctx context.Context, scope tenancy.Scope, operation marketplacegrowth.Operation) (marketplacegrowth.Operation, error) {
	if err := r.validate(ctx, scope); err != nil || operation.Validate() != nil {
		return marketplacegrowth.Operation{}, marketplacegrowth.ErrInvalid
	}
	document, err := json.Marshal(operation)
	if err != nil || forbiddenDocument(document) {
		return marketplacegrowth.Operation{}, marketplacegrowth.ErrInvalid
	}
	result := operation
	err = r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var existing []byte
		err := tx.QueryRowContext(ctx, `SELECT operation_document FROM marketplace_growth_operations WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, scope.OrganizationID(), scope.WorkspaceID(), operation.IdempotencyKey).Scan(&existing)
		if err == nil {
			if json.Unmarshal(existing, &result) != nil || result.InputDigest != operation.InputDigest || result.PreviewID != operation.PreviewID {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("marketplace growth repository: operation lookup: %w", err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO marketplace_growth_operations(organization_id,workspace_id,operation_id,preview_id,idempotency_key,approval_request_id,operation,capability,channel_id,account_id,target_id,input_digest,state,remote_write_qualified,remote_operation_id,error_code,operation_document,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17::jsonb,$18,$19)`, scope.OrganizationID(), scope.WorkspaceID(), operation.ID, operation.PreviewID, operation.IdempotencyKey, operation.ApprovalRequestID, operation.Operation, operation.Capability, operation.ChannelID, operation.AccountID, operation.TargetID, operation.InputDigest, operation.State, operation.RemoteWriteQualified, nullableString(operation.RemoteOperationID), nullableString(operation.ErrorCode), document, operation.CreatedAt, operation.UpdatedAt)
		return err
	})
	if errors.Is(err, ErrConflict) {
		return marketplacegrowth.Operation{}, marketplacegrowth.ErrConflict
	}
	return result, err
}

// Operation loads one durable intent.
func (r *Repository) Operation(ctx context.Context, scope tenancy.Scope, id string) (marketplacegrowth.Operation, error) {
	if err := r.validate(ctx, scope); err != nil || !validID(id) {
		return marketplacegrowth.Operation{}, marketplacegrowth.ErrInvalid
	}
	var result marketplacegrowth.Operation
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var document []byte
		if err := tx.QueryRowContext(ctx, `SELECT operation_document FROM marketplace_growth_operations WHERE organization_id=$1 AND workspace_id=$2 AND operation_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id).Scan(&document); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if json.Unmarshal(document, &result) != nil || result.Validate() != nil {
			return marketplacegrowth.ErrInvalid
		}
		return nil
	})
	return result, err
}

// ListOperations returns a bounded tenant-scoped operation list.
func (r *Repository) ListOperations(ctx context.Context, scope tenancy.Scope, limit int) ([]marketplacegrowth.Operation, error) {
	if err := r.validate(ctx, scope); err != nil || limit < 1 || limit > 200 {
		return nil, marketplacegrowth.ErrInvalid
	}
	result := make([]marketplacegrowth.Operation, 0, limit)
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT operation_document FROM marketplace_growth_operations WHERE organization_id=$1 AND workspace_id=$2 ORDER BY updated_at DESC,operation_id DESC LIMIT $3`, scope.OrganizationID(), scope.WorkspaceID(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var document []byte
			var operation marketplacegrowth.Operation
			if err := rows.Scan(&document); err != nil || json.Unmarshal(document, &operation) != nil || operation.Validate() != nil {
				return marketplacegrowth.ErrInvalid
			}
			result = append(result, operation)
		}
		return rows.Err()
	})
	return result, err
}

// SaveDrifts appends sanitized reconciliation findings idempotently.
func (r *Repository) SaveDrifts(ctx context.Context, scope tenancy.Scope, drifts []marketplacegrowth.Drift) error {
	if err := r.validate(ctx, scope); err != nil || len(drifts) > 200 {
		return marketplacegrowth.ErrInvalid
	}
	return r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		for _, drift := range drifts {
			if !validDrift(drift) {
				return marketplacegrowth.ErrInvalid
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_growth_drifts(organization_id,workspace_id,drift_id,operation_id,kind,expected_value,actual_value,severity,observed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(organization_id,workspace_id,drift_id) DO NOTHING`, scope.OrganizationID(), scope.WorkspaceID(), drift.ID, drift.OperationID, drift.Kind, drift.Expected, drift.Actual, drift.Severity, drift.ObservedAt); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListDrifts returns recent reconciliation evidence.
func (r *Repository) ListDrifts(ctx context.Context, scope tenancy.Scope, limit int) ([]marketplacegrowth.Drift, error) {
	if err := r.validate(ctx, scope); err != nil || limit < 1 || limit > 200 {
		return nil, marketplacegrowth.ErrInvalid
	}
	result := make([]marketplacegrowth.Drift, 0, limit)
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT drift_id,operation_id,kind,expected_value,actual_value,severity,observed_at FROM marketplace_growth_drifts WHERE organization_id=$1 AND workspace_id=$2 ORDER BY observed_at DESC,drift_id DESC LIMIT $3`, scope.OrganizationID(), scope.WorkspaceID(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var drift marketplacegrowth.Drift
			if err := rows.Scan(&drift.ID, &drift.OperationID, &drift.Kind, &drift.Expected, &drift.Actual, &drift.Severity, &drift.ObservedAt); err != nil {
				return err
			}
			result = append(result, drift)
		}
		return rows.Err()
	})
	return result, err
}

// SetKillSwitch changes the tenant-level control used by growth workers.
func (r *Repository) SetKillSwitch(ctx context.Context, scope tenancy.Scope, control marketplacegrowth.KillSwitch) error {
	if err := r.validate(ctx, scope); err != nil || control.Validate() != nil {
		return marketplacegrowth.ErrInvalid
	}
	return r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO marketplace_growth_controls(organization_id,workspace_id,kill_switch_enabled,reason,updated_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(organization_id,workspace_id) DO UPDATE SET kill_switch_enabled=EXCLUDED.kill_switch_enabled,reason=EXCLUDED.reason,updated_at=EXCLUDED.updated_at`, scope.OrganizationID(), scope.WorkspaceID(), control.Enabled, control.Reason, control.UpdatedAt)
		return err
	})
}

// KillSwitch returns the tenant-level control, defaulting to disabled when no
// explicit control has been written yet.
func (r *Repository) KillSwitch(ctx context.Context, scope tenancy.Scope) (marketplacegrowth.KillSwitch, error) {
	if err := r.validate(ctx, scope); err != nil {
		return marketplacegrowth.KillSwitch{}, err
	}
	result := marketplacegrowth.KillSwitch{}
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT kill_switch_enabled,reason,updated_at FROM marketplace_growth_controls WHERE organization_id=$1 AND workspace_id=$2`, scope.OrganizationID(), scope.WorkspaceID()).Scan(&result.Enabled, &result.Reason, &result.UpdatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	})
	return result, err
}

func (r *Repository) validate(ctx context.Context, scope tenancy.Scope) error {
	if ctx == nil || r == nil || r.db == nil || !scope.Valid() {
		return marketplacegrowth.ErrInvalid
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
		return marketplacegrowth.ErrInvalid
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func validID(value string) bool {
	return value != "" && len(value) <= 192 && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\x00\r\n/")
}

func forbiddenDocument(document []byte) bool {
	lower := strings.ToLower(string(document))
	for _, token := range []string{"authorization", "bearer ", "api-key", "private_key", "secret"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func validDrift(drift marketplacegrowth.Drift) bool {
	return validID(drift.ID) && validID(drift.OperationID) && validID(drift.Kind) && len(drift.Expected) <= 192 && len(drift.Actual) <= 192 && (drift.Severity == "low" || drift.Severity == "medium" || drift.Severity == "high" || drift.Severity == "critical") && !drift.ObservedAt.IsZero() && drift.ObservedAt.Location() == time.UTC
}
