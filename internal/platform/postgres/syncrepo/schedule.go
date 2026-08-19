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

const (
	previewColumns  = `id,connector_account_id,account_version,policy_count,read_count,write_count,created_at,expires_at,consumed_at`
	scheduleColumns = `connector_account_id,mode,interval_minutes,enabled,next_run_at,last_enqueued_at,coalesce(last_job_id,''),version,created_at,updated_at`
	jobColumns      = `id,organization_id,workspace_id,connector_account_id,kind,mode,status,coalesce(preview_id,''),coalesce(checkpoint_policy_id,''),started_runs,attempt_count,max_attempts,available_at,created_at,updated_at,started_at,completed_at,coalesce(last_error_code,'')`
)

// CreateBootstrapPreview persists immutable dry-run evidence. Reusing an ID is
// idempotent only when every immutable field matches.
func (r *Repository) CreateBootstrapPreview(ctx context.Context, scope tenancy.Scope, preview syncengine.BootstrapPreview) (syncengine.BootstrapPreview, error) {
	if err := r.validate(ctx, scope); err != nil {
		return syncengine.BootstrapPreview{}, err
	}
	if preview.Validate() != nil || preview.ConsumedAt != nil {
		return syncengine.BootstrapPreview{}, syncengine.ErrInvalidRecord
	}
	var out syncengine.BootstrapPreview
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO connector_bootstrap_previews(id,organization_id,workspace_id,connector_account_id,account_version,policy_count,read_count,write_count,created_at,expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`, preview.ID, scope.OrganizationID().String(), scope.WorkspaceID().String(), preview.AccountID, preview.AccountVersion, preview.PolicyCount, preview.ReadCount, preview.WriteCount, preview.CreatedAt, preview.ExpiresAt)
		if err != nil {
			return err
		}
		out, err = scanPreview(tx.QueryRowContext(ctx, `SELECT `+previewColumns+` FROM connector_bootstrap_previews WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), preview.ID))
		if err != nil {
			return err
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if inserted == 0 && !samePreview(out, preview) {
			return syncengine.ErrJobConflict
		}
		return nil
	})
	return out, err
}

// HasCurrentBootstrapPreview reports whether the account has unexpired preview
// evidence for its current optimistic version. Consumed evidence remains valid
// for configuring schedules until expiry.
func (r *Repository) HasCurrentBootstrapPreview(ctx context.Context, scope tenancy.Scope, accountID string, at time.Time) (bool, error) {
	if err := r.validate(ctx, scope); err != nil {
		return false, err
	}
	if accountID == "" || at.IsZero() || at.Location() != time.UTC {
		return false, syncengine.ErrInvalidRecord
	}
	var exists bool
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM connector_bootstrap_previews p JOIN connector_accounts a ON a.id=p.connector_account_id AND a.organization_id=p.organization_id AND a.workspace_id=p.workspace_id WHERE p.organization_id=$1 AND p.workspace_id=$2 AND p.connector_account_id=$3 AND p.account_version=a.version AND p.expires_at>$4)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID, at).Scan(&exists)
	})
	return exists, err
}

// CreateInitialJob atomically consumes a current preview and creates one
// durable initial-import job. The job ID is the caller's idempotency key.
func (r *Repository) CreateInitialJob(ctx context.Context, scope tenancy.Scope, previewID, jobID string, at time.Time) (syncengine.SyncJob, error) {
	if err := r.validate(ctx, scope); err != nil {
		return syncengine.SyncJob{}, err
	}
	if previewID == "" || jobID == "" || at.IsZero() || at.Location() != time.UTC {
		return syncengine.SyncJob{}, syncengine.ErrInvalidRecord
	}
	var out syncengine.SyncJob
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var err error
		out, err = scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM connector_sync_jobs WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), jobID))
		if err == nil {
			if out.Kind != syncengine.SyncJobInitialImport || out.PreviewID != previewID {
				return syncengine.ErrJobConflict
			}
			return nil
		}
		if !errors.Is(err, syncengine.ErrNotFound) {
			return err
		}
		preview, err := scanPreview(tx.QueryRowContext(ctx, `SELECT `+previewColumns+` FROM connector_bootstrap_previews p WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), previewID))
		if err != nil {
			return syncengine.ErrPreviewUnavailable
		}
		var version int64
		if err = tx.QueryRowContext(ctx, `SELECT version FROM connector_accounts WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), preview.AccountID).Scan(&version); err != nil || version != preview.AccountVersion || preview.ReadCount == 0 || !preview.ExpiresAt.After(at) || preview.ConsumedAt != nil {
			return syncengine.ErrPreviewUnavailable
		}
		out, err = scanJob(tx.QueryRowContext(ctx, `INSERT INTO connector_sync_jobs(id,organization_id,workspace_id,connector_account_id,kind,mode,status,preview_id,available_at,created_at,updated_at)
VALUES($1,$2,$3,$4,'initial_import','incremental','pending',$5,$6,$6,$6) RETURNING `+jobColumns, jobID, scope.OrganizationID().String(), scope.WorkspaceID().String(), preview.AccountID, preview.ID, at))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE connector_bootstrap_previews SET consumed_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), preview.ID, at)
		return err
	})
	return out, err
}

// PutAccountSchedule creates or optimistically replaces an account schedule.
func (r *Repository) PutAccountSchedule(ctx context.Context, scope tenancy.Scope, schedule syncengine.AccountSchedule, expectedVersion int64) (syncengine.AccountSchedule, error) {
	if err := r.validate(ctx, scope); err != nil {
		return syncengine.AccountSchedule{}, err
	}
	probe := schedule
	probe.Version = 1
	if probe.Validate() != nil || expectedVersion < 0 {
		return syncengine.AccountSchedule{}, syncengine.ErrInvalidRecord
	}
	var out syncengine.AccountSchedule
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var row scanner
		if expectedVersion == 0 {
			row = tx.QueryRowContext(ctx, `INSERT INTO connector_sync_schedules(organization_id,workspace_id,connector_account_id,mode,interval_minutes,enabled,next_run_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8) ON CONFLICT DO NOTHING RETURNING `+scheduleColumns, scope.OrganizationID().String(), scope.WorkspaceID().String(), schedule.AccountID, string(schedule.Mode), schedule.IntervalMinutes, schedule.Enabled, schedule.NextRunAt, schedule.CreatedAt)
		} else {
			row = tx.QueryRowContext(ctx, `UPDATE connector_sync_schedules SET mode=$4,interval_minutes=$5,enabled=$6,next_run_at=$7,version=version+1,updated_at=$8 WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3 AND version=$9 RETURNING `+scheduleColumns, scope.OrganizationID().String(), scope.WorkspaceID().String(), schedule.AccountID, string(schedule.Mode), schedule.IntervalMinutes, schedule.Enabled, schedule.NextRunAt, schedule.UpdatedAt, expectedVersion)
		}
		var err error
		out, err = scanSchedule(row)
		if errors.Is(err, syncengine.ErrNotFound) {
			return syncengine.ErrScheduleConflict
		}
		return err
	})
	return out, err
}

func (r *Repository) ListBootstrapPreviews(ctx context.Context, scope tenancy.Scope, limit int) ([]syncengine.BootstrapPreview, error) {
	if err := r.validate(ctx, scope); err != nil || limit < 1 || limit > 200 {
		if err != nil {
			return nil, err
		}
		return nil, syncengine.ErrInvalidRecord
	}
	out := make([]syncengine.BootstrapPreview, 0)
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+previewColumns+` FROM connector_bootstrap_previews WHERE organization_id=$1 AND workspace_id=$2 ORDER BY created_at DESC,id LIMIT $3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanPreview(rows)
			if e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

func (r *Repository) ListAccountSchedules(ctx context.Context, scope tenancy.Scope, limit int) ([]syncengine.AccountSchedule, error) {
	if err := r.validate(ctx, scope); err != nil || limit < 1 || limit > 200 {
		if err != nil {
			return nil, err
		}
		return nil, syncengine.ErrInvalidRecord
	}
	out := make([]syncengine.AccountSchedule, 0)
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+scheduleColumns+` FROM connector_sync_schedules WHERE organization_id=$1 AND workspace_id=$2 ORDER BY updated_at DESC,connector_account_id LIMIT $3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanSchedule(rows)
			if e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

func (r *Repository) ListSyncJobs(ctx context.Context, scope tenancy.Scope, limit int) ([]syncengine.SyncJob, error) {
	if err := r.validate(ctx, scope); err != nil || limit < 1 || limit > 200 {
		if err != nil {
			return nil, err
		}
		return nil, syncengine.ErrInvalidRecord
	}
	out := make([]syncengine.SyncJob, 0)
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+jobColumns+` FROM connector_sync_jobs WHERE organization_id=$1 AND workspace_id=$2 ORDER BY created_at DESC,id LIMIT $3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanJob(rows)
			if e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

// ClaimSyncJobs invokes the sole cross-tenant dispatch boundary. Returned jobs
// include tenant identity so every subsequent repository call is RLS scoped.
func (r *Repository) ClaimSyncJobs(ctx context.Context, workerID, leaseToken string, batch int, lease time.Duration) ([]syncengine.ClaimedSyncJob, error) {
	if ctx == nil || r == nil || r.database == nil || workerID == "" || leaseToken == "" || batch < 1 || batch > 100 || lease < 5*time.Second || lease > 5*time.Minute {
		return nil, syncengine.ErrInvalidRecord
	}
	rows, err := r.database.QueryContext(ctx, `SELECT * FROM claim_connector_sync_jobs($1,$2,$3,$4)`, workerID, leaseToken, batch, int(lease/time.Second))
	if err != nil {
		return nil, fmt.Errorf("sync repository: claim jobs: %w", err)
	}
	defer rows.Close()
	out := make([]syncengine.ClaimedSyncJob, 0)
	for rows.Next() {
		var v syncengine.ClaimedSyncJob
		var kind, mode, status string
		var preview, checkpoint, lastError sql.NullString
		if err = rows.Scan(&v.ID, &v.OrganizationID, &v.WorkspaceID, &v.AccountID, &kind, &mode, &status, &preview, &checkpoint, &v.StartedRuns, &v.AttemptCount, &v.MaxAttempts, &v.AvailableAt, &v.CreatedAt, &v.UpdatedAt, &v.StartedAt, &v.CompletedAt, &lastError, &v.LeaseToken, &v.LeaseUntil); err != nil {
			return nil, err
		}
		v.Kind = syncengine.SyncJobKind(kind)
		v.Mode = syncengine.ScheduleMode(mode)
		v.Status = syncengine.SyncJobStatus(status)
		if preview.Valid {
			v.PreviewID = preview.String
		}
		if checkpoint.Valid {
			v.CheckpointPolicyID = checkpoint.String
		}
		if lastError.Valid {
			v.LastErrorCode = lastError.String
		}
		if v.Validate() != nil {
			return nil, syncengine.ErrInvalidRecord
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) AdvanceSyncJob(ctx context.Context, scope tenancy.Scope, jobID, leaseToken, checkpointPolicyID string, startedRuns int, at time.Time) (syncengine.SyncJob, error) {
	return r.updateClaimedJob(ctx, scope, `UPDATE connector_sync_jobs SET checkpoint_policy_id=$5,started_runs=$6,updated_at=$7 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND lease_token=$4 AND status='running' AND lease_until>$7 RETURNING `+jobColumns, jobID, leaseToken, checkpointPolicyID, startedRuns, at)
}

func (r *Repository) CompleteSyncJob(ctx context.Context, scope tenancy.Scope, jobID, leaseToken string, at time.Time) (syncengine.SyncJob, error) {
	if err := r.validate(ctx, scope); err != nil {
		return syncengine.SyncJob{}, err
	}
	if jobID == "" || leaseToken == "" || at.IsZero() || at.Location() != time.UTC {
		return syncengine.SyncJob{}, syncengine.ErrInvalidRecord
	}
	var out syncengine.SyncJob
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var err error
		out, err = scanJob(tx.QueryRowContext(ctx, `UPDATE connector_sync_jobs SET status='completed',completed_at=$5,updated_at=$5,lease_owner=NULL,lease_token=NULL,lease_until=NULL,last_error_code=NULL WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND lease_token=$4 AND status='running' RETURNING `+jobColumns, scope.OrganizationID().String(), scope.WorkspaceID().String(), jobID, leaseToken, at))
		if errors.Is(err, syncengine.ErrNotFound) {
			return syncengine.ErrJobLeaseLost
		}
		return err
	})
	return out, err
}

func (r *Repository) ReleaseSyncJob(ctx context.Context, scope tenancy.Scope, jobID, leaseToken, errorCode string, retryAt, at time.Time, terminal bool) (syncengine.SyncJob, error) {
	if terminal {
		return r.updateClaimedJob(ctx, scope, `UPDATE connector_sync_jobs SET status='failed',completed_at=$5,updated_at=$5,lease_owner=NULL,lease_token=NULL,lease_until=NULL,last_error_code=$6 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND lease_token=$4 AND status='running' RETURNING `+jobColumns, jobID, leaseToken, errorCode, 0, at)
	}
	if retryAt.Before(at) {
		return syncengine.SyncJob{}, syncengine.ErrInvalidRecord
	}
	var out syncengine.SyncJob
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var err error
		out, err = scanJob(tx.QueryRowContext(ctx, `UPDATE connector_sync_jobs SET status='retry_wait',available_at=$5,updated_at=$6,lease_owner=NULL,lease_token=NULL,lease_until=NULL,last_error_code=$7 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND lease_token=$4 AND status='running' RETURNING `+jobColumns, scope.OrganizationID().String(), scope.WorkspaceID().String(), jobID, leaseToken, retryAt, at, errorCode))
		if errors.Is(err, syncengine.ErrNotFound) {
			return syncengine.ErrJobLeaseLost
		}
		return err
	})
	return out, err
}

func (r *Repository) updateClaimedJob(ctx context.Context, scope tenancy.Scope, query, jobID, leaseToken, value string, count int, at time.Time) (syncengine.SyncJob, error) {
	if err := r.validate(ctx, scope); err != nil {
		return syncengine.SyncJob{}, err
	}
	if jobID == "" || leaseToken == "" || at.IsZero() || at.Location() != time.UTC {
		return syncengine.SyncJob{}, syncengine.ErrInvalidRecord
	}
	var out syncengine.SyncJob
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var err error
		if count == 0 && value != "" {
			out, err = scanJob(tx.QueryRowContext(ctx, query, scope.OrganizationID().String(), scope.WorkspaceID().String(), jobID, leaseToken, at, value))
		} else {
			out, err = scanJob(tx.QueryRowContext(ctx, query, scope.OrganizationID().String(), scope.WorkspaceID().String(), jobID, leaseToken, value, count, at))
		}
		if errors.Is(err, syncengine.ErrNotFound) {
			return syncengine.ErrJobLeaseLost
		}
		return err
	})
	return out, err
}

func scanPreview(row scanner) (syncengine.BootstrapPreview, error) {
	var v syncengine.BootstrapPreview
	if err := row.Scan(&v.ID, &v.AccountID, &v.AccountVersion, &v.PolicyCount, &v.ReadCount, &v.WriteCount, &v.CreatedAt, &v.ExpiresAt, &v.ConsumedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v, syncengine.ErrNotFound
		}
		return v, err
	}
	if v.Validate() != nil {
		return v, syncengine.ErrInvalidRecord
	}
	return v, nil
}
func scanSchedule(row scanner) (syncengine.AccountSchedule, error) {
	var v syncengine.AccountSchedule
	var mode string
	if err := row.Scan(&v.AccountID, &mode, &v.IntervalMinutes, &v.Enabled, &v.NextRunAt, &v.LastEnqueuedAt, &v.LastJobID, &v.Version, &v.CreatedAt, &v.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v, syncengine.ErrNotFound
		}
		return v, err
	}
	v.Mode = syncengine.ScheduleMode(mode)
	if v.Validate() != nil {
		return v, syncengine.ErrInvalidRecord
	}
	return v, nil
}
func scanJob(row scanner) (syncengine.SyncJob, error) {
	var v syncengine.SyncJob
	var kind, mode, status string
	if err := row.Scan(&v.ID, &v.OrganizationID, &v.WorkspaceID, &v.AccountID, &kind, &mode, &status, &v.PreviewID, &v.CheckpointPolicyID, &v.StartedRuns, &v.AttemptCount, &v.MaxAttempts, &v.AvailableAt, &v.CreatedAt, &v.UpdatedAt, &v.StartedAt, &v.CompletedAt, &v.LastErrorCode); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v, syncengine.ErrNotFound
		}
		return v, err
	}
	v.Kind = syncengine.SyncJobKind(kind)
	v.Mode = syncengine.ScheduleMode(mode)
	v.Status = syncengine.SyncJobStatus(status)
	if v.Validate() != nil {
		return v, syncengine.ErrInvalidRecord
	}
	return v, nil
}
func samePreview(a, b syncengine.BootstrapPreview) bool {
	return a.ID == b.ID && a.AccountID == b.AccountID && a.AccountVersion == b.AccountVersion && a.PolicyCount == b.PolicyCount && a.ReadCount == b.ReadCount && a.WriteCount == b.WriteCount
}
