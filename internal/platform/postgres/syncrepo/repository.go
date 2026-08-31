// Package syncrepo implements Task-013 sync control-plane persistence in PostgreSQL.
package syncrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

const applyScopeStatement = `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`

type Repository struct{ database *sql.DB }

var _ syncengine.Repository = (*Repository)(nil)

func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("sync repository: database is required")
	}
	return &Repository{database: database}, nil
}

func (r *Repository) CreatePolicy(ctx context.Context, scope tenancy.Scope, c syncengine.PolicyCreate) (syncengine.Policy, error) {
	if err := r.validate(ctx, scope); err != nil {
		return syncengine.Policy{}, err
	}
	if c.Validate() != nil {
		return syncengine.Policy{}, syncengine.ErrInvalidRecord
	}
	var out syncengine.Policy
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `INSERT INTO sync_policies (id,organization_id,workspace_id,connector_account_id,entity_type,direction,source_of_truth,enabled)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING id,organization_id,workspace_id,connector_account_id,entity_type,direction,source_of_truth,enabled,version,created_at,updated_at`, c.ID, scope.OrganizationID().String(), scope.WorkspaceID().String(), c.ConnectorAccountID, c.EntityType, string(c.Direction), string(c.SourceOfTruth), c.Enabled)
		var err error
		out, err = scanPolicy(row)
		if err != nil {
			return mapPolicyWriteErr(err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO sync_checkpoints (organization_id,workspace_id,policy_id,cursor,version,updated_at) VALUES ($1,$2,$3,'',1,$4)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), out.ID, out.CreatedAt)
		return err
	})
	return out, err
}
func (r *Repository) UpdatePolicy(ctx context.Context, scope tenancy.Scope, u syncengine.PolicyUpdate) (syncengine.Policy, error) {
	if err := r.validate(ctx, scope); err != nil {
		return syncengine.Policy{}, err
	}
	if u.Validate() != nil {
		return syncengine.Policy{}, syncengine.ErrInvalidRecord
	}
	var out syncengine.Policy
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var err error
		out, err = scanPolicy(tx.QueryRowContext(ctx, `UPDATE sync_policies SET direction=$4,source_of_truth=$5,enabled=$6,version=version+1,updated_at=clock_timestamp()
WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$7
RETURNING id,organization_id,workspace_id,connector_account_id,entity_type,direction,source_of_truth,enabled,version,created_at,updated_at`, scope.OrganizationID().String(), scope.WorkspaceID().String(), u.ID, string(u.Direction), string(u.SourceOfTruth), u.Enabled, u.ExpectedVersion))
		if errors.Is(err, syncengine.ErrNotFound) {
			return syncengine.ErrPolicyConflict
		}
		return err
	})
	return out, err
}
func (r *Repository) Policy(ctx context.Context, scope tenancy.Scope, id string) (syncengine.Policy, error) {
	if err := r.validate(ctx, scope); err != nil {
		return syncengine.Policy{}, err
	}
	var out syncengine.Policy
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		out, err = scanPolicy(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,connector_account_id,entity_type,direction,source_of_truth,enabled,version,created_at,updated_at FROM sync_policies WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id))
		if errors.Is(err, syncengine.ErrNotFound) {
			return syncengine.ErrPolicyNotFound
		}
		return err
	})
	return out, err
}

// ListPolicies returns the current workspace policies in stable newest-first order.
func (r *Repository) ListPolicies(ctx context.Context, scope tenancy.Scope, limit int) ([]syncengine.Policy, error) {
	if err := r.validate(ctx, scope); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 200 {
		return nil, syncengine.ErrInvalidRecord
	}
	out := make([]syncengine.Policy, 0)
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,organization_id,workspace_id,connector_account_id,entity_type,direction,source_of_truth,enabled,version,created_at,updated_at FROM sync_policies WHERE organization_id=$1 AND workspace_id=$2 ORDER BY updated_at DESC,id LIMIT $3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			policy, err := scanPolicy(rows)
			if err != nil {
				return err
			}
			out = append(out, policy)
		}
		return rows.Err()
	})
	return out, err
}
func (r *Repository) Checkpoint(ctx context.Context, scope tenancy.Scope, policyID string) (syncengine.Checkpoint, error) {
	if err := r.validate(ctx, scope); err != nil {
		return syncengine.Checkpoint{}, err
	}
	var out syncengine.Checkpoint
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		out, err = scanCheckpoint(tx.QueryRowContext(ctx, `SELECT policy_id,cursor,version,updated_at FROM sync_checkpoints WHERE organization_id=$1 AND workspace_id=$2 AND policy_id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), policyID))
		return err
	})
	return out, err
}
func (r *Repository) AdvanceCheckpoint(ctx context.Context, scope tenancy.Scope, policyID string, expected int64, cursor string, at time.Time) (syncengine.Checkpoint, error) {
	if err := r.validate(ctx, scope); err != nil {
		return syncengine.Checkpoint{}, err
	}
	if expected < 1 || at.IsZero() || at.Location() != time.UTC {
		return syncengine.Checkpoint{}, syncengine.ErrInvalidRecord
	}
	var out syncengine.Checkpoint
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var err error
		out, err = scanCheckpoint(tx.QueryRowContext(ctx, `UPDATE sync_checkpoints SET cursor=$4,version=version+1,updated_at=$5 WHERE organization_id=$1 AND workspace_id=$2 AND policy_id=$3 AND version=$6 RETURNING policy_id,cursor,version,updated_at`, scope.OrganizationID().String(), scope.WorkspaceID().String(), policyID, cursor, at, expected))
		if errors.Is(err, syncengine.ErrNotFound) {
			return syncengine.ErrCheckpointConflict
		}
		return err
	})
	return out, err
}
func (r *Repository) EntityState(ctx context.Context, scope tenancy.Scope, policyID, localID string) (syncengine.EntityState, error) {
	if err := r.validate(ctx, scope); err != nil {
		return syncengine.EntityState{}, err
	}
	var out syncengine.EntityState
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		out, err = scanState(tx.QueryRowContext(ctx, `SELECT policy_id,local_entity_id,remote_id,last_local_version,last_remote_revision,last_synced_fingerprint,last_local_event_id,last_remote_change_id,version,updated_at FROM sync_entity_states WHERE organization_id=$1 AND workspace_id=$2 AND policy_id=$3 AND local_entity_id=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), policyID, localID))
		return err
	})
	return out, err
}
func (r *Repository) SaveEntityState(ctx context.Context, scope tenancy.Scope, s syncengine.EntityState, expected int64) (syncengine.EntityState, error) {
	if err := r.validate(ctx, scope); err != nil {
		return syncengine.EntityState{}, err
	}
	probe := s
	probe.Version = 1
	if probe.Validate() != nil || expected < 0 {
		return syncengine.EntityState{}, syncengine.ErrInvalidRecord
	}
	var out syncengine.EntityState
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var row *sql.Row
		if expected == 0 {
			row = tx.QueryRowContext(ctx, `INSERT INTO sync_entity_states (organization_id,workspace_id,policy_id,local_entity_id,remote_id,last_local_version,last_remote_revision,last_synced_fingerprint,last_local_event_id,last_remote_change_id,version,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,1,$11) ON CONFLICT DO NOTHING
RETURNING policy_id,local_entity_id,remote_id,last_local_version,last_remote_revision,last_synced_fingerprint,last_local_event_id,last_remote_change_id,version,updated_at`, scope.OrganizationID().String(), scope.WorkspaceID().String(), s.PolicyID, s.LocalEntityID, s.RemoteID, s.LastLocalVersion, s.LastRemoteRevision, s.LastSyncedFingerprint, s.LastLocalEventID, nullString(s.LastRemoteChangeID), s.UpdatedAt)
		} else {
			row = tx.QueryRowContext(ctx, `UPDATE sync_entity_states SET last_local_version=$6,last_remote_revision=$7,last_synced_fingerprint=$8,last_local_event_id=$9,last_remote_change_id=$10,version=version+1,updated_at=$11
WHERE organization_id=$1 AND workspace_id=$2 AND policy_id=$3 AND local_entity_id=$4 AND remote_id=$5 AND version=$12
RETURNING policy_id,local_entity_id,remote_id,last_local_version,last_remote_revision,last_synced_fingerprint,last_local_event_id,last_remote_change_id,version,updated_at`, scope.OrganizationID().String(), scope.WorkspaceID().String(), s.PolicyID, s.LocalEntityID, s.RemoteID, s.LastLocalVersion, s.LastRemoteRevision, s.LastSyncedFingerprint, s.LastLocalEventID, nullString(s.LastRemoteChangeID), s.UpdatedAt, expected)
		}
		var err error
		out, err = scanState(row)
		if errors.Is(err, syncengine.ErrNotFound) {
			return syncengine.ErrStateConflict
		}
		return err
	})
	return out, err
}
func (r *Repository) LocalReceipt(ctx context.Context, scope tenancy.Scope, p, id string) (syncengine.Receipt, error) {
	return r.receipt(ctx, scope, "sync_local_receipts", p, id)
}
func (r *Repository) RemoteReceipt(ctx context.Context, scope tenancy.Scope, p, id string) (syncengine.Receipt, error) {
	return r.receipt(ctx, scope, "sync_remote_receipts", p, id)
}
func (r *Repository) RecordLocalReceipt(ctx context.Context, scope tenancy.Scope, v syncengine.Receipt) error {
	return r.recordReceipt(ctx, scope, "sync_local_receipts", v)
}
func (r *Repository) RecordRemoteReceipt(ctx context.Context, scope tenancy.Scope, v syncengine.Receipt) error {
	return r.recordReceipt(ctx, scope, "sync_remote_receipts", v)
}

func (r *Repository) receipt(ctx context.Context, scope tenancy.Scope, table, p, id string) (syncengine.Receipt, error) {
	if err := r.validate(ctx, scope); err != nil {
		return syncengine.Receipt{}, err
	}
	if table != "sync_local_receipts" && table != "sync_remote_receipts" {
		return syncengine.Receipt{}, syncengine.ErrInvalidRecord
	}
	var out syncengine.Receipt
	q := `SELECT policy_id,change_id,fingerprint,outcome,created_at FROM ` + table + ` WHERE organization_id=$1 AND workspace_id=$2 AND policy_id=$3 AND change_id=$4`
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		out, err = scanReceipt(tx.QueryRowContext(ctx, q, scope.OrganizationID().String(), scope.WorkspaceID().String(), p, id))
		return err
	})
	return out, err
}
func (r *Repository) recordReceipt(ctx context.Context, scope tenancy.Scope, table string, v syncengine.Receipt) error {
	if err := r.validate(ctx, scope); err != nil {
		return err
	}
	if v.Validate() != nil || (table != "sync_local_receipts" && table != "sync_remote_receipts") {
		return syncengine.ErrInvalidRecord
	}
	return r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		q := `INSERT INTO ` + table + ` (organization_id,workspace_id,policy_id,change_id,fingerprint,outcome,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`
		result, err := tx.ExecContext(ctx, q, scope.OrganizationID().String(), scope.WorkspaceID().String(), v.PolicyID, v.ChangeID, v.Fingerprint, string(v.Outcome), v.CreatedAt)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 1 {
			return nil
		}
		existing, err := scanReceipt(tx.QueryRowContext(ctx, `SELECT policy_id,change_id,fingerprint,outcome,created_at FROM `+table+` WHERE organization_id=$1 AND workspace_id=$2 AND policy_id=$3 AND change_id=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), v.PolicyID, v.ChangeID))
		if err != nil {
			return err
		}
		if existing.Fingerprint != v.Fingerprint {
			return syncengine.ErrReceiptCollision
		}
		return nil
	})
}
func (r *Repository) validate(ctx context.Context, scope tenancy.Scope) error {
	if ctx == nil {
		return errors.New("sync repository: context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.database == nil {
		return errors.New("sync repository: repository is not initialized")
	}
	if !scope.Valid() {
		return tenancy.ErrInvalidScope
	}
	return nil
}
func (r *Repository) withTx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("sync repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var org, ws string
	if err := tx.QueryRowContext(ctx, applyScopeStatement, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &ws); err != nil {
		return fmt.Errorf("sync repository: scope: %w", err)
	}
	if org != scope.OrganizationID().String() || ws != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sync repository: commit: %w", err)
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanPolicy(row scanner) (syncengine.Policy, error) {
	var p syncengine.Policy
	var d, s string
	if err := row.Scan(&p.ID, &p.OrganizationID, &p.WorkspaceID, &p.ConnectorAccountID, &p.EntityType, &d, &s, &p.Enabled, &p.Version, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return p, syncengine.ErrNotFound
		}
		return p, err
	}
	p.Direction = syncengine.Direction(d)
	p.SourceOfTruth = syncengine.SourceOfTruth(s)
	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()
	if p.Validate() != nil {
		return p, syncengine.ErrInvalidRecord
	}
	return p, nil
}
func scanCheckpoint(row scanner) (syncengine.Checkpoint, error) {
	var v syncengine.Checkpoint
	if err := row.Scan(&v.PolicyID, &v.Cursor, &v.Version, &v.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v, syncengine.ErrNotFound
		}
		return v, err
	}
	v.UpdatedAt = v.UpdatedAt.UTC()
	if v.Validate() != nil {
		return v, syncengine.ErrInvalidRecord
	}
	return v, nil
}
func scanState(row scanner) (syncengine.EntityState, error) {
	var v syncengine.EntityState
	var remote sql.NullString
	if err := row.Scan(&v.PolicyID, &v.LocalEntityID, &v.RemoteID, &v.LastLocalVersion, &v.LastRemoteRevision, &v.LastSyncedFingerprint, &v.LastLocalEventID, &remote, &v.Version, &v.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v, syncengine.ErrNotFound
		}
		return v, err
	}
	if remote.Valid {
		v.LastRemoteChangeID = remote.String
	}
	v.UpdatedAt = v.UpdatedAt.UTC()
	if v.Validate() != nil {
		return v, syncengine.ErrInvalidRecord
	}
	return v, nil
}
func scanReceipt(row scanner) (syncengine.Receipt, error) {
	var v syncengine.Receipt
	var outcome string
	if err := row.Scan(&v.PolicyID, &v.ChangeID, &v.Fingerprint, &outcome, &v.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v, syncengine.ErrNotFound
		}
		return v, err
	}
	v.Outcome = syncengine.Outcome(outcome)
	v.CreatedAt = v.CreatedAt.UTC()
	if v.Validate() != nil {
		return v, syncengine.ErrInvalidRecord
	}
	return v, nil
}
func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func mapPolicyWriteErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return syncengine.ErrPolicyConflict
	}
	return err
}
