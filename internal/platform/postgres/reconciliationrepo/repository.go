// Package reconciliationrepo persists Task-014 reconciliation control-plane state.
package reconciliationrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/reconciliation"
)

const applyScopeStatement = `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`

type Repository struct{ db *sql.DB }

var _ reconciliation.Repository = (*Repository)(nil)

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("reconciliation repository: database required")
	}
	return &Repository{db: db}, nil
}
func (r *Repository) CreateRun(ctx context.Context, s tenancy.Scope, v reconciliation.Run) (reconciliation.Run, error) {
	if err := r.valid(ctx, s); err != nil {
		return reconciliation.Run{}, err
	}
	if v.Validate() != nil {
		return reconciliation.Run{}, reconciliation.ErrInvalid
	}
	var out reconciliation.Run
	err := r.tx(ctx, s, false, func(tx *sql.Tx) error {
		var e error
		out, e = scanRun(tx.QueryRowContext(ctx, `INSERT INTO reconciliation_runs(id,organization_id,workspace_id,policy_id,mode,trigger_ref,status,cursor,scanned_count,drift_count,version,started_at,updated_at,completed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id,policy_id,mode,trigger_ref,status,cursor,scanned_count,drift_count,version,started_at,updated_at,completed_at`, v.ID, s.OrganizationID().String(), s.WorkspaceID().String(), v.PolicyID, string(v.Mode), null(v.TriggerRef), string(v.Status), v.Cursor, v.ScannedCount, v.DriftCount, v.Version, v.StartedAt, v.UpdatedAt, v.CompletedAt))
		return e
	})
	return out, err
}
func (r *Repository) Run(ctx context.Context, s tenancy.Scope, id string) (reconciliation.Run, error) {
	if err := r.valid(ctx, s); err != nil {
		return reconciliation.Run{}, err
	}
	var out reconciliation.Run
	err := r.tx(ctx, s, true, func(tx *sql.Tx) error {
		var e error
		out, e = scanRun(tx.QueryRowContext(ctx, `SELECT id,policy_id,mode,trigger_ref,status,cursor,scanned_count,drift_count,version,started_at,updated_at,completed_at FROM reconciliation_runs WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, s.OrganizationID().String(), s.WorkspaceID().String(), id))
		return e
	})
	return out, err
}

// ListRuns returns recent reconciliation runs for the current workspace.
func (r *Repository) ListRuns(ctx context.Context, s tenancy.Scope, limit int) ([]reconciliation.Run, error) {
	if err := r.valid(ctx, s); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 200 {
		return nil, reconciliation.ErrInvalid
	}
	out := make([]reconciliation.Run, 0)
	err := r.tx(ctx, s, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,policy_id,mode,trigger_ref,status,cursor,scanned_count,drift_count,version,started_at,updated_at,completed_at FROM reconciliation_runs WHERE organization_id=$1 AND workspace_id=$2 ORDER BY started_at DESC,id LIMIT $3`, s.OrganizationID().String(), s.WorkspaceID().String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			run, err := scanRun(rows)
			if err != nil {
				return err
			}
			out = append(out, run)
		}
		return rows.Err()
	})
	return out, err
}

// ListRecentDrifts returns recent drift evidence without exposing remote payloads.
func (r *Repository) ListRecentDrifts(ctx context.Context, s tenancy.Scope, limit int) ([]reconciliation.Drift, error) {
	if err := r.valid(ctx, s); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 200 {
		return nil, reconciliation.ErrInvalid
	}
	out := make([]reconciliation.Drift, 0)
	err := r.tx(ctx, s, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,run_id,policy_id,kind,local_entity_id,remote_id,local_fingerprint,remote_fingerprint,local_status,remote_status,local_version,remote_revision,mapping_local_count,mapping_remote_count,detected_at,status,recommended_action,version,resolved_at FROM reconciliation_drifts WHERE organization_id=$1 AND workspace_id=$2 ORDER BY detected_at DESC,id LIMIT $3`, s.OrganizationID().String(), s.WorkspaceID().String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			drift, err := scanDrift(rows)
			if err != nil {
				return err
			}
			out = append(out, drift)
		}
		return rows.Err()
	})
	return out, err
}
func (r *Repository) UpdateRun(ctx context.Context, s tenancy.Scope, v reconciliation.Run, expected int64) (reconciliation.Run, error) {
	if err := r.valid(ctx, s); err != nil {
		return reconciliation.Run{}, err
	}
	probe := v
	probe.Version = 1
	if probe.Validate() != nil || expected < 1 {
		return reconciliation.Run{}, reconciliation.ErrInvalid
	}
	var out reconciliation.Run
	err := r.tx(ctx, s, false, func(tx *sql.Tx) error {
		var e error
		out, e = scanRun(tx.QueryRowContext(ctx, `UPDATE reconciliation_runs SET status=$4,cursor=$5,scanned_count=$6,drift_count=$7,version=version+1,updated_at=$8,completed_at=$9 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$10 RETURNING id,policy_id,mode,trigger_ref,status,cursor,scanned_count,drift_count,version,started_at,updated_at,completed_at`, s.OrganizationID().String(), s.WorkspaceID().String(), v.ID, string(v.Status), v.Cursor, v.ScannedCount, v.DriftCount, v.UpdatedAt, v.CompletedAt, expected))
		if errors.Is(e, reconciliation.ErrNotFound) {
			return reconciliation.ErrConflict
		}
		return e
	})
	return out, err
}
func (r *Repository) RecordDrift(ctx context.Context, s tenancy.Scope, v reconciliation.Drift) (reconciliation.Drift, bool, error) {
	if err := r.valid(ctx, s); err != nil {
		return reconciliation.Drift{}, false, err
	}
	if v.Validate() != nil {
		return reconciliation.Drift{}, false, reconciliation.ErrInvalid
	}
	var out reconciliation.Drift
	inserted := false
	err := r.tx(ctx, s, false, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `INSERT INTO reconciliation_drifts(id,organization_id,workspace_id,run_id,policy_id,kind,local_entity_id,remote_id,local_fingerprint,remote_fingerprint,local_status,remote_status,local_version,remote_revision,mapping_local_count,mapping_remote_count,detected_at,status,recommended_action,version,resolved_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21) ON CONFLICT DO NOTHING RETURNING id,run_id,policy_id,kind,local_entity_id,remote_id,local_fingerprint,remote_fingerprint,local_status,remote_status,local_version,remote_revision,mapping_local_count,mapping_remote_count,detected_at,status,recommended_action,version,resolved_at`, v.ID, s.OrganizationID().String(), s.WorkspaceID().String(), v.RunID, v.PolicyID, string(v.Kind), null(v.LocalEntityID), null(v.RemoteID), null(v.LocalFingerprint), null(v.RemoteFingerprint), null(v.LocalStatus), null(v.RemoteStatus), v.LocalVersion, null(v.RemoteRevision), v.MappingLocalCount, v.MappingRemoteCount, v.DetectedAt, string(v.Status), string(v.RecommendedAction), v.Version, v.ResolvedAt)
		got, e := scanDrift(row)
		if e == nil {
			out = got
			inserted = true
			return nil
		}
		if !errors.Is(e, reconciliation.ErrNotFound) {
			return e
		}
		out, e = scanDrift(tx.QueryRowContext(ctx, `SELECT id,run_id,policy_id,kind,local_entity_id,remote_id,local_fingerprint,remote_fingerprint,local_status,remote_status,local_version,remote_revision,mapping_local_count,mapping_remote_count,detected_at,status,recommended_action,version,resolved_at FROM reconciliation_drifts WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, s.OrganizationID().String(), s.WorkspaceID().String(), v.ID))
		return e
	})
	return out, inserted, err
}
func (r *Repository) Drift(ctx context.Context, s tenancy.Scope, id string) (reconciliation.Drift, error) {
	if err := r.valid(ctx, s); err != nil {
		return reconciliation.Drift{}, err
	}
	var out reconciliation.Drift
	err := r.tx(ctx, s, true, func(tx *sql.Tx) error {
		var e error
		out, e = scanDrift(tx.QueryRowContext(ctx, `SELECT id,run_id,policy_id,kind,local_entity_id,remote_id,local_fingerprint,remote_fingerprint,local_status,remote_status,local_version,remote_revision,mapping_local_count,mapping_remote_count,detected_at,status,recommended_action,version,resolved_at FROM reconciliation_drifts WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, s.OrganizationID().String(), s.WorkspaceID().String(), id))
		return e
	})
	return out, err
}
func (r *Repository) UpdateDrift(ctx context.Context, s tenancy.Scope, v reconciliation.Drift, expected int64) (reconciliation.Drift, error) {
	if err := r.valid(ctx, s); err != nil {
		return reconciliation.Drift{}, err
	}
	probe := v
	probe.Version = 1
	if probe.Validate() != nil || expected < 1 {
		return reconciliation.Drift{}, reconciliation.ErrInvalid
	}
	var out reconciliation.Drift
	err := r.tx(ctx, s, false, func(tx *sql.Tx) error {
		var e error
		out, e = scanDrift(tx.QueryRowContext(ctx, `UPDATE reconciliation_drifts SET status=$4,version=version+1,resolved_at=$5 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$6 RETURNING id,run_id,policy_id,kind,local_entity_id,remote_id,local_fingerprint,remote_fingerprint,local_status,remote_status,local_version,remote_revision,mapping_local_count,mapping_remote_count,detected_at,status,recommended_action,version,resolved_at`, s.OrganizationID().String(), s.WorkspaceID().String(), v.ID, string(v.Status), v.ResolvedAt, expected))
		if errors.Is(e, reconciliation.ErrNotFound) {
			return reconciliation.ErrConflict
		}
		return e
	})
	return out, err
}
func (r *Repository) ListDrifts(ctx context.Context, s tenancy.Scope, runID string, limit int) ([]reconciliation.Drift, error) {
	if err := r.valid(ctx, s); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		return nil, reconciliation.ErrInvalid
	}
	out := []reconciliation.Drift{}
	err := r.tx(ctx, s, true, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT id,run_id,policy_id,kind,local_entity_id,remote_id,local_fingerprint,remote_fingerprint,local_status,remote_status,local_version,remote_revision,mapping_local_count,mapping_remote_count,detected_at,status,recommended_action,version,resolved_at FROM reconciliation_drifts WHERE organization_id=$1 AND workspace_id=$2 AND run_id=$3 ORDER BY detected_at,id LIMIT $4`, s.OrganizationID().String(), s.WorkspaceID().String(), runID, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanDrift(rows)
			if e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}
func (r *Repository) RecordAction(ctx context.Context, s tenancy.Scope, v reconciliation.ActionRecord) error {
	if err := r.valid(ctx, s); err != nil {
		return err
	}
	if v.Validate() != nil {
		return reconciliation.ErrInvalid
	}
	return r.tx(ctx, s, false, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO reconciliation_actions(id,organization_id,workspace_id,drift_id,action,idempotency_key,result,error_code,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, v.ID, s.OrganizationID().String(), s.WorkspaceID().String(), v.DriftID, string(v.Action), v.IdempotencyKey, string(v.Result), null(v.ErrorCode), v.CreatedAt)
		return e
	})
}
func (r *Repository) ListActions(ctx context.Context, s tenancy.Scope, driftID string, limit int) ([]reconciliation.ActionRecord, error) {
	if err := r.valid(ctx, s); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		return nil, reconciliation.ErrInvalid
	}
	out := []reconciliation.ActionRecord{}
	err := r.tx(ctx, s, true, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT id,drift_id,action,idempotency_key,result,error_code,created_at FROM reconciliation_actions WHERE organization_id=$1 AND workspace_id=$2 AND drift_id=$3 ORDER BY created_at,id LIMIT $4`, s.OrganizationID().String(), s.WorkspaceID().String(), driftID, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanAction(rows)
			if e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}
func (r *Repository) valid(ctx context.Context, s tenancy.Scope) error {
	if ctx == nil {
		return errors.New("reconciliation repository: context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.db == nil {
		return errors.New("reconciliation repository: uninitialized")
	}
	if !s.Valid() {
		return tenancy.ErrInvalidScope
	}
	return nil
}
func (r *Repository) tx(ctx context.Context, s tenancy.Scope, read bool, fn func(*sql.Tx) error) error {
	tx, e := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: read})
	if e != nil {
		return fmt.Errorf("reconciliation repository: begin: %w", e)
	}
	defer func() { _ = tx.Rollback() }()
	var o, w string
	if e = tx.QueryRowContext(ctx, applyScopeStatement, s.OrganizationID().String(), s.WorkspaceID().String()).Scan(&o, &w); e != nil {
		return e
	}
	if o != s.OrganizationID().String() || w != s.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}

type scanner interface{ Scan(...any) error }

func scanRun(row scanner) (reconciliation.Run, error) {
	var v reconciliation.Run
	var mode, status string
	var trigger sql.NullString
	var completed sql.NullTime
	if e := row.Scan(&v.ID, &v.PolicyID, &mode, &trigger, &status, &v.Cursor, &v.ScannedCount, &v.DriftCount, &v.Version, &v.StartedAt, &v.UpdatedAt, &completed); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return v, reconciliation.ErrNotFound
		}
		return v, e
	}
	v.Mode = reconciliation.Mode(mode)
	v.Status = reconciliation.RunStatus(status)
	v.StartedAt = v.StartedAt.UTC()
	v.UpdatedAt = v.UpdatedAt.UTC()
	if trigger.Valid {
		v.TriggerRef = trigger.String
	}
	if completed.Valid {
		t := completed.Time.UTC()
		v.CompletedAt = &t
	}
	if v.Validate() != nil {
		return v, reconciliation.ErrInvalid
	}
	return v, nil
}
func scanDrift(row scanner) (reconciliation.Drift, error) {
	var v reconciliation.Drift
	var kind, status, action string
	var l, r, lf, rf, ls, rs, rev sql.NullString
	var resolved sql.NullTime
	if e := row.Scan(&v.ID, &v.RunID, &v.PolicyID, &kind, &l, &r, &lf, &rf, &ls, &rs, &v.LocalVersion, &rev, &v.MappingLocalCount, &v.MappingRemoteCount, &v.DetectedAt, &status, &action, &v.Version, &resolved); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return v, reconciliation.ErrNotFound
		}
		return v, e
	}
	v.Kind = reconciliation.DriftKind(kind)
	v.Status = reconciliation.DriftStatus(status)
	v.RecommendedAction = reconciliation.ActionKind(action)
	v.DetectedAt = v.DetectedAt.UTC()
	if l.Valid {
		v.LocalEntityID = l.String
	}
	if r.Valid {
		v.RemoteID = r.String
	}
	if lf.Valid {
		v.LocalFingerprint = lf.String
	}
	if rf.Valid {
		v.RemoteFingerprint = rf.String
	}
	if ls.Valid {
		v.LocalStatus = ls.String
	}
	if rs.Valid {
		v.RemoteStatus = rs.String
	}
	if rev.Valid {
		v.RemoteRevision = rev.String
	}
	if resolved.Valid {
		t := resolved.Time.UTC()
		v.ResolvedAt = &t
	}
	if v.Validate() != nil {
		return v, reconciliation.ErrInvalid
	}
	return v, nil
}
func scanAction(row scanner) (reconciliation.ActionRecord, error) {
	var v reconciliation.ActionRecord
	var a, res string
	var ec sql.NullString
	if e := row.Scan(&v.ID, &v.DriftID, &a, &v.IdempotencyKey, &res, &ec, &v.CreatedAt); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return v, reconciliation.ErrNotFound
		}
		return v, e
	}
	v.Action = reconciliation.ActionKind(a)
	v.Result = reconciliation.ActionResult(res)
	v.CreatedAt = v.CreatedAt.UTC()
	if ec.Valid {
		v.ErrorCode = ec.String
	}
	if v.Validate() != nil {
		return v, reconciliation.ErrInvalid
	}
	return v, nil
}
func null(v string) any {
	if v == "" {
		return nil
	}
	return v
}
