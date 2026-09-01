// Package workflowrepo persists the tenant-scoped workflow control plane.
package workflowrepo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/workflow"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`

var workflowIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

type Repository struct{ db *sql.DB }

// Transaction is the narrow SQL surface used when an event trigger is
// persisted inside the caller-owned Inbox transaction. Keeping this boundary
// separate from Repository.tx makes the run insert and the immutable Inbox
// receipt commit atomically.
type Transaction interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("workflow repository: database required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Create(ctx context.Context, scope workflow.Scope, id string, definition workflow.Definition) (workflow.Workflow, error) {
	if err := valid(ctx, r, scope); err != nil || definition.Validate() != nil || !workflowIDValid(id) {
		return workflow.Workflow{}, workflow.ErrInvalid
	}
	plan, err := workflow.Compile(definition)
	if err != nil {
		return workflow.Workflow{}, err
	}
	defJSON, err := json.Marshal(definition)
	if err != nil {
		return workflow.Workflow{}, workflow.ErrInvalid
	}
	planJSON, err := json.Marshal(plan.NodeIDs)
	if err != nil {
		return workflow.Workflow{}, workflow.ErrInvalid
	}
	now := time.Now().UTC()
	var out workflow.Workflow
	err = r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2, 0))`, scope.OrganizationID(), scope.WorkspaceID()); err != nil {
			return err
		}
		// Resource creation is replay-safe even though the public command only
		// carries an Idempotency-Key. The API derives the default workflow ID
		// from that key; a same-definition retry returns the existing resource,
		// while reusing an ID for different semantics is a conflict.
		var existing workflow.Workflow
		existingErr := scanWorkflow(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,name,description,status,current_version,version,created_at,updated_at FROM workflows WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id), &existing)
		if existingErr == nil {
			var existingDigest string
			if err := tx.QueryRowContext(ctx, `SELECT plan_digest FROM workflow_versions WHERE organization_id=$1 AND workspace_id=$2 AND workflow_id=$3 AND version=$4`, scope.OrganizationID(), scope.WorkspaceID(), id, existing.CurrentVersion).Scan(&existingDigest); err != nil {
				return err
			}
			if existing.Name == definition.Name && existing.Description == definition.Description && existingDigest == plan.Digest {
				out = existing
				return nil
			}
			return workflow.ErrConflict
		}
		if !errors.Is(existingErr, workflow.ErrNotFound) {
			return existingErr
		}
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM workflows WHERE organization_id=$1 AND workspace_id=$2 AND status<>'archived'`, scope.OrganizationID(), scope.WorkspaceID()).Scan(&active); err != nil {
			return err
		}
		if active >= 100 {
			return workflow.ErrQuotaExceeded
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO workflows(organization_id,workspace_id,id,name,description,status,current_version,version,trigger_kind,trigger_event_type,trigger_interval_minutes,trigger_enabled,next_run_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'draft',1,1,$6,$7,$8,$9,$10,$11,$11)`, scope.OrganizationID(), scope.WorkspaceID(), id, definition.Name, definition.Description, string(definition.Trigger.Kind), definition.Trigger.EventType, definition.Trigger.IntervalMinutes, definition.Trigger.Enabled, definition.Trigger.NextRunAt, now); err != nil {
			return mapWriteError(err)
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO workflow_versions(organization_id,workspace_id,id,workflow_id,version,definition,plan_node_ids,plan_digest,created_at) VALUES($1,$2,$3,$4,1,$5::jsonb,$6::jsonb,$7,$8)`, scope.OrganizationID(), scope.WorkspaceID(), id+"@1", id, string(defJSON), string(planJSON), plan.Digest, now)
		if err != nil {
			return mapWriteError(err)
		}
		out = workflow.Workflow{ID: id, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Name: definition.Name, Description: definition.Description, Status: workflow.StatusDraft, CurrentVersion: 1, Version: 1, CreatedAt: now, UpdatedAt: now}
		return nil
	})
	return out, err
}

func (r *Repository) List(ctx context.Context, scope workflow.Scope, limit int) ([]workflow.Workflow, error) {
	if err := valid(ctx, r, scope); err != nil || limit < 1 || limit > 200 {
		return nil, workflow.ErrInvalid
	}
	items := make([]workflow.Workflow, 0)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,organization_id,workspace_id,name,description,status,current_version,version,created_at,updated_at FROM workflows WHERE organization_id=$1 AND workspace_id=$2 ORDER BY updated_at DESC,id LIMIT $3`, scope.OrganizationID(), scope.WorkspaceID(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item workflow.Workflow
			err := scanWorkflow(rows, &item)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

func (r *Repository) Get(ctx context.Context, scope workflow.Scope, id string) (workflow.Workflow, error) {
	if err := valid(ctx, r, scope); err != nil || id == "" {
		return workflow.Workflow{}, workflow.ErrInvalid
	}
	var out workflow.Workflow
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		return scanWorkflow(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,name,description,status,current_version,version,created_at,updated_at FROM workflows WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id), &out)
	})
	return out, err
}

func (r *Repository) Version(ctx context.Context, scope workflow.Scope, id string, version int64) (workflow.WorkflowVersion, error) {
	if err := valid(ctx, r, scope); err != nil || id == "" || version < 1 {
		return workflow.WorkflowVersion{}, workflow.ErrInvalid
	}
	var out workflow.WorkflowVersion
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var definition, nodeIDs []byte
		var published *time.Time
		err := tx.QueryRowContext(ctx, `SELECT id,workflow_id,organization_id,workspace_id,version,definition,plan_node_ids,plan_digest,created_at,published_at FROM workflow_versions WHERE organization_id=$1 AND workspace_id=$2 AND workflow_id=$3 AND version=$4`, scope.OrganizationID(), scope.WorkspaceID(), id, version).Scan(&out.ID, &out.WorkflowID, &out.OrganizationID, &out.WorkspaceID, &out.Version, &definition, &nodeIDs, &out.PlanDigest, &out.CreatedAt, &published)
		if err != nil {
			return mapReadError(err)
		}
		if err := json.Unmarshal(definition, &out.Definition); err != nil {
			return fmt.Errorf("workflow repository: decode definition: %w", err)
		}
		out.PublishedAt = published
		return out.Validate()
	})
	return out, err
}

// Publish validates and atomically appends an immutable version.
func (r *Repository) Publish(ctx context.Context, scope workflow.Scope, id string, expectedVersion int64, definition workflow.Definition) (workflow.Workflow, error) {
	if err := valid(ctx, r, scope); err != nil || expectedVersion < 1 || definition.Validate() != nil {
		return workflow.Workflow{}, workflow.ErrInvalid
	}
	plan, err := workflow.Compile(definition)
	if err != nil {
		return workflow.Workflow{}, err
	}
	defJSON, _ := json.Marshal(definition)
	planJSON, _ := json.Marshal(plan.NodeIDs)
	now := time.Now().UTC()
	var out workflow.Workflow
	err = r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var current int64
		var status workflow.DefinitionStatus
		if err := tx.QueryRowContext(ctx, `SELECT current_version,status FROM workflows WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), id).Scan(&current, &status); err != nil {
			return mapReadError(err)
		}
		if current != expectedVersion || (status != workflow.StatusDraft && status != workflow.StatusPaused && status != workflow.StatusPublished) {
			return workflow.ErrConflict
		}
		next := current + 1
		if _, err := tx.ExecContext(ctx, `INSERT INTO workflow_versions(organization_id,workspace_id,id,workflow_id,version,definition,plan_node_ids,plan_digest,created_at,published_at) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,$9)`, scope.OrganizationID(), scope.WorkspaceID(), fmt.Sprintf("%s@%d", id, next), id, next, string(defJSON), string(planJSON), plan.Digest, now); err != nil {
			return mapWriteError(err)
		}
		if err := tx.QueryRowContext(ctx, `UPDATE workflows SET name=$4,description=$5,status='published',current_version=$6,version=version+1,trigger_kind=$7,trigger_event_type=$8,trigger_interval_minutes=$9,trigger_enabled=$10,next_run_at=$11,updated_at=$12 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 RETURNING id,organization_id,workspace_id,name,description,status,current_version,version,created_at,updated_at`, scope.OrganizationID(), scope.WorkspaceID(), id, definition.Name, definition.Description, next, string(definition.Trigger.Kind), definition.Trigger.EventType, definition.Trigger.IntervalMinutes, definition.Trigger.Enabled, definition.Trigger.NextRunAt, now).Scan(&out.ID, &out.OrganizationID, &out.WorkspaceID, &out.Name, &out.Description, &out.Status, &out.CurrentVersion, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
			return mapWriteError(err)
		}
		return nil
	})
	return out, err
}

func (r *Repository) ChangeStatus(ctx context.Context, scope workflow.Scope, id string, expectedVersion int64, next workflow.DefinitionStatus) (workflow.Workflow, error) {
	if err := valid(ctx, r, scope); err != nil || id == "" || expectedVersion < 1 || !next.Valid() {
		return workflow.Workflow{}, workflow.ErrInvalid
	}
	var out workflow.Workflow
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var current workflow.DefinitionStatus
		if err := tx.QueryRowContext(ctx, `SELECT status FROM workflows WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$4`, scope.OrganizationID(), scope.WorkspaceID(), id, expectedVersion).Scan(&current); err != nil {
			return mapReadError(err)
		}
		if err := workflow.ValidateTransition(current, next); err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := tx.QueryRowContext(ctx, `UPDATE workflows SET status=$4,version=version+1,updated_at=$5 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$6 RETURNING id,organization_id,workspace_id,name,description,status,current_version,version,created_at,updated_at`, scope.OrganizationID(), scope.WorkspaceID(), id, string(next), now, expectedVersion).Scan(&out.ID, &out.OrganizationID, &out.WorkspaceID, &out.Name, &out.Description, &out.Status, &out.CurrentVersion, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
			return workflow.ErrConflict
		}
		return nil
	})
	return out, err
}

func (r *Repository) CreateRun(ctx context.Context, scope workflow.Scope, req workflow.RunRequest) (workflow.Run, error) {
	if err := valid(ctx, r, scope); err != nil || req.Validate() != nil {
		return workflow.Run{}, workflow.ErrInvalid
	}
	if _, err := workflow.ParseScope(scope.OrganizationID(), scope.WorkspaceID()); err != nil {
		return workflow.Run{}, workflow.ErrInvalid
	}
	now := time.Now().UTC()
	var out workflow.Run
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2, 0))`, scope.OrganizationID(), scope.WorkspaceID()); err != nil {
			return err
		}
		// Resolve the idempotency key before applying quotas.  A retried command
		// must replay the original run even when the workspace is currently at
		// its concurrency limit; a different request under the same key is a
		// deterministic conflict rather than a database 500.
		var existing workflow.Run
		err := scanRun(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,workflow_id,workflow_version,trigger_kind,trigger_ref,idempotency_key,input_digest,status,attempt_count,available_at,started_at,completed_at,last_error_code,version FROM workflow_runs WHERE organization_id=$1 AND workspace_id=$2 AND workflow_id=$3 AND idempotency_key=$4`, scope.OrganizationID(), scope.WorkspaceID(), req.WorkflowID, req.IdempotencyKey), &existing)
		if err == nil {
			if sameRunRequest(existing, scope, req) {
				out = existing
				return nil
			}
			return workflow.ErrConflict
		}
		if !errors.Is(err, workflow.ErrNotFound) {
			return err
		}
		var recent, concurrent int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE available_at>=clock_timestamp()-interval '1 minute'), count(*) FILTER (WHERE status IN ('queued','running','waiting_approval','waiting_retry')) FROM workflow_runs WHERE organization_id=$1 AND workspace_id=$2`, scope.OrganizationID(), scope.WorkspaceID()).Scan(&recent, &concurrent); err != nil {
			return err
		}
		if recent >= 120 || concurrent >= 8 {
			return workflow.ErrQuotaExceeded
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO workflow_runs(organization_id,workspace_id,id,workflow_id,workflow_version,trigger_kind,trigger_ref,idempotency_key,input_digest,status,available_at,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'queued',$10,1) ON CONFLICT (organization_id,workspace_id,workflow_id,idempotency_key) DO NOTHING RETURNING id,organization_id,workspace_id,workflow_id,workflow_version,trigger_kind,trigger_ref,idempotency_key,input_digest,status,attempt_count,available_at,started_at,completed_at,last_error_code,version`, scope.OrganizationID(), scope.WorkspaceID(), req.ID, req.WorkflowID, req.WorkflowVersion, string(req.TriggerKind), req.TriggerRef, req.IdempotencyKey, req.InputDigest, now).Scan(&out.ID, &out.OrganizationID, &out.WorkspaceID, &out.WorkflowID, &out.WorkflowVersion, &out.TriggerKind, &out.TriggerRef, &out.IdempotencyKey, &out.InputDigest, &out.Status, &out.AttemptCount, &out.AvailableAt, &out.StartedAt, &out.CompletedAt, &out.LastErrorCode, &out.Version); err == nil {
			return nil
		} else if !errors.Is(err, workflow.ErrNotFound) {
			return mapWriteError(err)
		}
		// A concurrent insert won the unique key.  Re-read it in this
		// transaction and apply the same semantic equality check.
		if err := scanRun(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,workflow_id,workflow_version,trigger_kind,trigger_ref,idempotency_key,input_digest,status,attempt_count,available_at,started_at,completed_at,last_error_code,version FROM workflow_runs WHERE organization_id=$1 AND workspace_id=$2 AND workflow_id=$3 AND idempotency_key=$4`, scope.OrganizationID(), scope.WorkspaceID(), req.WorkflowID, req.IdempotencyKey), &existing); err != nil {
			return err
		}
		if !sameRunRequest(existing, scope, req) {
			return workflow.ErrConflict
		}
		out = existing
		return nil
	})
	return out, err
}

func sameRunRequest(existing workflow.Run, scope workflow.Scope, req workflow.RunRequest) bool {
	return existing.ID == req.ID && existing.OrganizationID == scope.OrganizationID() && existing.WorkspaceID == scope.WorkspaceID() && existing.WorkflowID == req.WorkflowID && existing.WorkflowVersion == req.WorkflowVersion && existing.TriggerKind == req.TriggerKind && existing.TriggerRef == req.TriggerRef && existing.IdempotencyKey == req.IdempotencyKey && existing.InputDigest == req.InputDigest
}

func (r *Repository) GetRun(ctx context.Context, scope workflow.Scope, id string) (workflow.Run, error) {
	if err := valid(ctx, r, scope); err != nil || id == "" {
		return workflow.Run{}, workflow.ErrInvalid
	}
	var out workflow.Run
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		return scanRun(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,workflow_id,workflow_version,trigger_kind,trigger_ref,idempotency_key,input_digest,status,attempt_count,available_at,started_at,completed_at,last_error_code,version FROM workflow_runs WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id), &out)
	})
	return out, err
}

func (r *Repository) ListRuns(ctx context.Context, scope workflow.Scope, limit int) ([]workflow.Run, error) {
	if err := valid(ctx, r, scope); err != nil || limit < 1 || limit > 200 {
		return nil, workflow.ErrInvalid
	}
	items := make([]workflow.Run, 0)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,organization_id,workspace_id,workflow_id,workflow_version,trigger_kind,trigger_ref,idempotency_key,input_digest,status,attempt_count,available_at,started_at,completed_at,last_error_code,version FROM workflow_runs WHERE organization_id=$1 AND workspace_id=$2 ORDER BY available_at DESC,id DESC LIMIT $3`, scope.OrganizationID(), scope.WorkspaceID(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item workflow.Run
			if err := scanRun(rows, &item); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

func (r *Repository) UpdateRun(ctx context.Context, scope workflow.Scope, item workflow.Run, expectedVersion int64) (workflow.Run, error) {
	if err := valid(ctx, r, scope); err != nil || item.Validate() != nil || expectedVersion < 1 || item.OrganizationID != scope.OrganizationID() || item.WorkspaceID != scope.WorkspaceID() {
		return workflow.Run{}, workflow.ErrInvalid
	}
	var out workflow.Run
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		return scanRun(tx.QueryRowContext(ctx, `UPDATE workflow_runs SET status=$4,attempt_count=$5,available_at=$6,started_at=$7,completed_at=$8,last_error_code=$9,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$10 RETURNING id,organization_id,workspace_id,workflow_id,workflow_version,trigger_kind,trigger_ref,idempotency_key,input_digest,status,attempt_count,available_at,started_at,completed_at,last_error_code,version`, scope.OrganizationID(), scope.WorkspaceID(), item.ID, string(item.Status), item.AttemptCount, item.AvailableAt, item.StartedAt, item.CompletedAt, item.LastErrorCode, expectedVersion), &out)
	})
	if errors.Is(err, workflow.ErrNotFound) {
		return workflow.Run{}, workflow.ErrConflict
	}
	return out, err
}

// ClaimedRun is a fenced lease returned to one worker.
type ClaimedRun struct {
	Run        workflow.Run
	LeaseToken string
}

// DispatchDueSchedules creates at most one bounded run per due workflow. The
// row lock and idempotency key make scheduler restarts safe without using Kafka
// as a delayed-job store.
func (r *Repository) DispatchDueSchedules(ctx context.Context, scope workflow.Scope, batch int) error {
	if err := valid(ctx, r, scope); err != nil || batch < 1 || batch > 100 {
		return workflow.ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `WITH capacity AS (SELECT count(*) FILTER (WHERE available_at>=clock_timestamp()-interval '1 minute') AS recent, count(*) FILTER (WHERE status IN ('queued','running','waiting_approval','waiting_retry')) AS active FROM workflow_runs WHERE organization_id=$1 AND workspace_id=$2), due AS (SELECT w.id,w.current_version,w.next_run_at,w.trigger_interval_minutes FROM workflows w CROSS JOIN capacity WHERE capacity.recent<120 AND w.organization_id=$1 AND w.workspace_id=$2 AND w.status='published' AND w.trigger_kind='schedule' AND w.trigger_enabled AND w.next_run_at IS NOT NULL AND w.next_run_at<=clock_timestamp() ORDER BY w.next_run_at,w.id FOR UPDATE SKIP LOCKED LIMIT LEAST($3,GREATEST(0,8-capacity.active))) SELECT id,current_version,next_run_at,trigger_interval_minutes FROM due`, scope.OrganizationID(), scope.WorkspaceID(), batch)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var workflowID string
			var version, minutes int64
			var due time.Time
			if err := rows.Scan(&workflowID, &version, &due, &minutes); err != nil {
				return err
			}
			key := fmt.Sprintf("schedule:%s:%s", workflowID, due.UTC().Format(time.RFC3339Nano))
			runID := "run_" + fmt.Sprintf("%x", sha256Bytes([]byte(key)))[:32]
			inputDigest := fmt.Sprintf("%x", sha256Bytes([]byte(key)))
			if _, err := tx.ExecContext(ctx, `INSERT INTO workflow_runs(organization_id,workspace_id,id,workflow_id,workflow_version,trigger_kind,trigger_ref,idempotency_key,input_digest,status,available_at,version) VALUES($1,$2,$3,$4,$5,'schedule',$6,$7,$8,'queued',clock_timestamp(),1) ON CONFLICT (organization_id,workspace_id,workflow_id,idempotency_key) DO NOTHING`, scope.OrganizationID(), scope.WorkspaceID(), runID, workflowID, version, due.UTC().Format(time.RFC3339Nano), key, inputDigest); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE workflows SET next_run_at=GREATEST(next_run_at+make_interval(mins=>$4),clock_timestamp()),version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), workflowID, minutes); err != nil {
				return err
			}
		}
		return rows.Err()
	})
}

// ClaimRuns claims due runs for one tenant without exposing raw trigger data.
func (r *Repository) ClaimRuns(ctx context.Context, scope workflow.Scope, workerID, leaseToken string, batch int, lease time.Duration) ([]ClaimedRun, error) {
	if err := valid(ctx, r, scope); err != nil || workerID == "" || leaseToken == "" || batch < 1 || batch > 100 || lease <= 0 || lease > time.Hour {
		return nil, workflow.ErrInvalid
	}
	claimed := make([]ClaimedRun, 0, batch)
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM workflow_runs WHERE organization_id=$1 AND workspace_id=$2 AND status IN ('queued','running','waiting_approval','waiting_retry')`, scope.OrganizationID(), scope.WorkspaceID()).Scan(&active); err != nil {
			return err
		}
		claimLimit := batch
		if remaining := 8 - active; claimLimit > remaining {
			claimLimit = remaining
		}
		if claimLimit <= 0 {
			return nil
		}
		// PostgreSQL does not allow a correlated value in LIMIT. Calculate the
		// bounded concurrency budget in the same transaction, then pass the
		// resulting scalar limit to the locked claim query.
		rows, err := tx.QueryContext(ctx, `WITH due AS (SELECT w.id FROM workflow_runs w WHERE w.organization_id=$1 AND w.workspace_id=$2 AND w.status IN ('queued','running','waiting_retry','waiting_approval') AND w.available_at<=clock_timestamp() AND (w.lease_until IS NULL OR w.lease_until<clock_timestamp()) ORDER BY w.available_at,w.id FOR UPDATE SKIP LOCKED LIMIT $3) UPDATE workflow_runs w SET lease_token=$4,lease_until=clock_timestamp()+$5::interval,version=version+1 FROM due WHERE w.organization_id=$1 AND w.workspace_id=$2 AND w.id=due.id RETURNING w.id,w.organization_id,w.workspace_id,w.workflow_id,w.workflow_version,w.trigger_kind,w.trigger_ref,w.idempotency_key,w.input_digest,w.status,w.attempt_count,w.available_at,w.started_at,w.completed_at,w.last_error_code,w.version`, scope.OrganizationID(), scope.WorkspaceID(), claimLimit, leaseToken, intervalLiteral(lease))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item workflow.Run
			if err := scanRun(rows, &item); err != nil {
				return err
			}
			claimed = append(claimed, ClaimedRun{Run: item, LeaseToken: leaseToken})
		}
		return rows.Err()
	})
	return claimed, err
}

// ReleaseRun fences a worker and schedules a retry or terminal failure.
func (r *Repository) ReleaseRun(ctx context.Context, scope workflow.Scope, id, leaseToken string, status workflow.RunStatus, availableAt time.Time, errorCode string) error {
	if err := valid(ctx, r, scope); err != nil || id == "" || leaseToken == "" || !status.Valid() || !isUTC(availableAt) || (errorCode != "" && !validErrorCode(errorCode)) {
		return workflow.ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE workflow_runs SET status=$4,available_at=$5,last_error_code=$6,completed_at=CASE WHEN $4 IN ('completed','failed','cancelled') THEN clock_timestamp() ELSE completed_at END,lease_token=NULL,lease_until=NULL,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND lease_token=$7`, scope.OrganizationID(), scope.WorkspaceID(), id, string(status), availableAt, errorCode, leaseToken)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return workflow.ErrConflict
		}
		return nil
	})
}

// CancelRun is an operator recovery operation. It never deletes the run or
// evidence and fences any active lease.
func (r *Repository) CancelRun(ctx context.Context, scope workflow.Scope, id string, expectedVersion int64) (workflow.Run, error) {
	if err := valid(ctx, r, scope); err != nil || id == "" || expectedVersion < 1 {
		return workflow.Run{}, workflow.ErrInvalid
	}
	var out workflow.Run
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		return scanRun(tx.QueryRowContext(ctx, `UPDATE workflow_runs SET status='cancelled',completed_at=clock_timestamp(),lease_token=NULL,lease_until=NULL,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$4 AND status NOT IN ('completed','failed','cancelled') RETURNING id,organization_id,workspace_id,workflow_id,workflow_version,trigger_kind,trigger_ref,idempotency_key,input_digest,status,attempt_count,available_at,started_at,completed_at,last_error_code,version`, scope.OrganizationID(), scope.WorkspaceID(), id, expectedVersion), &out)
	})
	if errors.Is(err, workflow.ErrNotFound) {
		return workflow.Run{}, workflow.ErrConflict
	}
	return out, err
}

// Step returns one node state, or ErrNotFound when it has not started.
func (r *Repository) Step(ctx context.Context, scope workflow.Scope, runID, nodeID string) (workflow.StepRun, error) {
	if err := valid(ctx, r, scope); err != nil || runID == "" || nodeID == "" {
		return workflow.StepRun{}, workflow.ErrInvalid
	}
	var out workflow.StepRun
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		return scanStep(tx.QueryRowContext(ctx, `SELECT id,run_id,organization_id,workspace_id,node_id,status,attempt_count,output_digest,error_code,started_at,completed_at,version FROM workflow_step_runs WHERE organization_id=$1 AND workspace_id=$2 AND run_id=$3 AND node_id=$4`, scope.OrganizationID(), scope.WorkspaceID(), runID, nodeID), &out)
	})
	return out, err
}

// ListSteps returns bounded step state for an operator run timeline.
func (r *Repository) ListSteps(ctx context.Context, scope workflow.Scope, runID string, limit int) ([]workflow.StepRun, error) {
	if err := valid(ctx, r, scope); err != nil || runID == "" || limit < 1 || limit > 200 {
		return nil, workflow.ErrInvalid
	}
	items := make([]workflow.StepRun, 0)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,run_id,organization_id,workspace_id,node_id,status,attempt_count,output_digest,error_code,started_at,completed_at,version FROM workflow_step_runs WHERE organization_id=$1 AND workspace_id=$2 AND run_id=$3 ORDER BY id LIMIT $4`, scope.OrganizationID(), scope.WorkspaceID(), runID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item workflow.StepRun
			if err := scanStep(rows, &item); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// ListEvidence returns immutable, bounded evidence in observation order.
func (r *Repository) ListEvidence(ctx context.Context, scope workflow.Scope, runID string, limit int) ([]workflow.ExecutionEvidence, error) {
	if err := valid(ctx, r, scope); err != nil || runID == "" || limit < 1 || limit > 200 {
		return nil, workflow.ErrInvalid
	}
	items := make([]workflow.ExecutionEvidence, 0)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,run_id,organization_id,workspace_id,node_id,attempt,outcome,output_digest,error_code,observed_at FROM workflow_step_evidence WHERE organization_id=$1 AND workspace_id=$2 AND run_id=$3 ORDER BY observed_at,id LIMIT $4`, scope.OrganizationID(), scope.WorkspaceID(), runID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item workflow.ExecutionEvidence
			var outcome string
			if err := rows.Scan(&item.ID, &item.RunID, &item.OrganizationID, &item.WorkspaceID, &item.NodeID, &item.Attempt, &outcome, &item.OutputDigest, &item.ErrorCode, &item.ObservedAt); err != nil {
				return err
			}
			item.Outcome = workflow.StepStatus(outcome)
			if err := item.Validate(); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// UpsertStep creates a queued step or updates it with an optimistic version.
func (r *Repository) UpsertStep(ctx context.Context, scope workflow.Scope, item workflow.StepRun, expectedVersion int64) (workflow.StepRun, error) {
	if err := valid(ctx, r, scope); err != nil || item.Validate() != nil || item.OrganizationID != scope.OrganizationID() || item.WorkspaceID != scope.WorkspaceID() {
		return workflow.StepRun{}, workflow.ErrInvalid
	}
	var out workflow.StepRun
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		if expectedVersion == 0 {
			return scanStep(tx.QueryRowContext(ctx, `INSERT INTO workflow_step_runs(organization_id,workspace_id,id,run_id,node_id,status,attempt_count,output_digest,error_code,started_at,completed_at,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,1) ON CONFLICT (organization_id,workspace_id,run_id,node_id) DO NOTHING RETURNING id,run_id,organization_id,workspace_id,node_id,status,attempt_count,output_digest,error_code,started_at,completed_at,version`, scope.OrganizationID(), scope.WorkspaceID(), item.ID, item.RunID, item.NodeID, string(item.Status), item.AttemptCount, item.OutputDigest, item.ErrorCode, item.StartedAt, item.CompletedAt), &out)
		}
		return scanStep(tx.QueryRowContext(ctx, `UPDATE workflow_step_runs SET status=$6,attempt_count=$7,output_digest=$8,error_code=$9,started_at=$10,completed_at=$11,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$4 RETURNING id,run_id,organization_id,workspace_id,node_id,status,attempt_count,output_digest,error_code,started_at,completed_at,version`, scope.OrganizationID(), scope.WorkspaceID(), item.ID, expectedVersion, item.RunID, string(item.Status), item.AttemptCount, item.OutputDigest, item.ErrorCode, item.StartedAt, item.CompletedAt), &out)
	})
	return out, err
}

func (r *Repository) AppendEvidence(ctx context.Context, scope workflow.Scope, id, runID, nodeID string, attempt int, outcome workflow.StepStatus, outputDigest, errorCode string, observedAt time.Time) error {
	if err := valid(ctx, r, scope); err != nil || id == "" || runID == "" || nodeID == "" || attempt < 1 || attempt > 64 || !isEvidenceOutcome(outcome) || !observedAt.UTC().Equal(observedAt) || outputDigest != "" && !hexDigest(outputDigest) || errorCode != "" && !validErrorCode(errorCode) {
		return workflow.ErrInvalid
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO workflow_step_evidence(organization_id,workspace_id,id,run_id,node_id,attempt,outcome,output_digest,error_code,observed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, scope.OrganizationID(), scope.WorkspaceID(), id, runID, nodeID, attempt, string(outcome), outputDigest, errorCode, observedAt)
		return mapWriteError(err)
	})
}

// TriggerEvent creates at most one run per published workflow and event id.
func (r *Repository) TriggerEvent(ctx context.Context, scope workflow.Scope, event eventbus.Event) ([]workflow.Run, error) {
	if err := valid(ctx, r, scope); err != nil || event.ID == "" || event.Type.Validate() != nil {
		return nil, workflow.ErrInvalid
	}
	created := make([]workflow.Run, 0)
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var err error
		created, err = r.triggerEventTransaction(ctx, scope, tx, event)
		return err
	})
	return created, err
}

// TriggerEventInTransaction persists event-triggered runs in an existing
// Inbox-owned transaction. The caller commits the transaction and its receipt
// together, so a crash cannot leave a run without deduplication evidence (or a
// receipt without its corresponding run).
func (r *Repository) TriggerEventInTransaction(ctx context.Context, scope workflow.Scope, tx Transaction, event eventbus.Event) ([]workflow.Run, error) {
	if err := valid(ctx, r, scope); err != nil || tx == nil || event.ID == "" || event.Type.Validate() != nil {
		return nil, workflow.ErrInvalid
	}
	if _, err := tx.ExecContext(ctx, applyScope, scope.OrganizationID(), scope.WorkspaceID()); err != nil {
		return nil, err
	}
	return r.triggerEventTransaction(ctx, scope, tx, event)
}

func (r *Repository) triggerEventTransaction(ctx context.Context, scope workflow.Scope, tx Transaction, event eventbus.Event) ([]workflow.Run, error) {
	digest := fmt.Sprintf("%x", sha256Bytes(event.Data))
	created := make([]workflow.Run, 0)
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2, 0))`, scope.OrganizationID(), scope.WorkspaceID()); err != nil {
		return nil, err
	}
	var recent, active int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE available_at>=clock_timestamp()-interval '1 minute'), count(*) FILTER (WHERE status IN ('queued','running','waiting_approval','waiting_retry')) FROM workflow_runs WHERE organization_id=$1 AND workspace_id=$2`, scope.OrganizationID(), scope.WorkspaceID()).Scan(&recent, &active); err != nil {
		return nil, err
	}
	newRuns := 0
	rows, err := tx.QueryContext(ctx, `SELECT id,current_version FROM workflows WHERE organization_id=$1 AND workspace_id=$2 AND status='published' AND trigger_kind='event' AND trigger_enabled AND trigger_event_type=$3 ORDER BY id`, scope.OrganizationID(), scope.WorkspaceID(), string(event.Type))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var workflowID string
		var version int64
		if err := rows.Scan(&workflowID, &version); err != nil {
			return nil, err
		}
		if recent >= 120 || active+newRuns >= 8 {
			return nil, workflow.ErrQuotaExceeded
		}
		runID := "run_" + digest[:32] + fmt.Sprintf("_%x", sha256Bytes([]byte(workflowID))[:8])
		result, err := tx.ExecContext(ctx, `INSERT INTO workflow_runs(organization_id,workspace_id,id,workflow_id,workflow_version,trigger_kind,trigger_ref,idempotency_key,input_digest,status,available_at,version) VALUES($1,$2,$3,$4,$5,'event',$6,$7,$8,'queued',clock_timestamp(),1) ON CONFLICT (organization_id,workspace_id,workflow_id,idempotency_key) DO NOTHING`, scope.OrganizationID(), scope.WorkspaceID(), runID, workflowID, version, event.ID, event.ID, digest)
		if err != nil {
			return nil, err
		}
		if inserted, rowsErr := result.RowsAffected(); rowsErr == nil && inserted == 1 {
			newRuns++
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO workflow_event_receipts(organization_id,workspace_id,event_id,workflow_id,workflow_version,run_id) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, scope.OrganizationID(), scope.WorkspaceID(), event.ID, workflowID, version, runID); err != nil {
			return nil, err
		}
		var item workflow.Run
		if err := scanRun(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,workflow_id,workflow_version,trigger_kind,trigger_ref,idempotency_key,input_digest,status,attempt_count,available_at,started_at,completed_at,last_error_code,version FROM workflow_runs WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), runID), &item); err == nil {
			created = append(created, item)
		} else if !errors.Is(err, workflow.ErrNotFound) {
			return nil, err
		}
	}
	return created, rows.Err()
}

func (r *Repository) tx(ctx context.Context, scope workflow.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	if err := valid(ctx, r, scope); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, func() *sql.TxOptions {
		if readOnly {
			return &sql.TxOptions{ReadOnly: true}
		}
		return &sql.TxOptions{}
	}())
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, applyScope, scope.OrganizationID(), scope.WorkspaceID()); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func valid(ctx context.Context, r *Repository, scope workflow.Scope) error {
	if ctx == nil || r == nil || r.db == nil || !scope.Valid() {
		return workflow.ErrInvalid
	}
	return nil
}

func workflowIDValid(value string) bool {
	return workflowIDPattern.MatchString(value)
}

func isEvidenceOutcome(status workflow.StepStatus) bool {
	return status == workflow.StepCompleted || status == workflow.StepFailed || status == workflow.StepSkipped || status == workflow.StepWaitingApproval || status == workflow.StepWaitingRetry
}

type scanner interface{ Scan(...any) error }

func scanWorkflow(s scanner, out ...*workflow.Workflow) error {
	var item workflow.Workflow
	err := s.Scan(&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.Name, &item.Description, &item.Status, &item.CurrentVersion, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return workflow.ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := item.Validate(); err != nil {
		return err
	}
	if len(out) > 0 {
		*out[0] = item
	}
	return nil
}
func scanRun(s scanner, out *workflow.Run) error {
	err := s.Scan(&out.ID, &out.OrganizationID, &out.WorkspaceID, &out.WorkflowID, &out.WorkflowVersion, &out.TriggerKind, &out.TriggerRef, &out.IdempotencyKey, &out.InputDigest, &out.Status, &out.AttemptCount, &out.AvailableAt, &out.StartedAt, &out.CompletedAt, &out.LastErrorCode, &out.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return workflow.ErrNotFound
	}
	if err != nil {
		return err
	}
	return out.Validate()
}
func mapReadError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return workflow.ErrNotFound
	}
	return err
}
func mapWriteError(err error) error {
	if err == nil {
		return nil
	}
	return err
}

func scanStep(s scanner, out *workflow.StepRun) error {
	err := s.Scan(&out.ID, &out.RunID, &out.OrganizationID, &out.WorkspaceID, &out.NodeID, &out.Status, &out.AttemptCount, &out.OutputDigest, &out.ErrorCode, &out.StartedAt, &out.CompletedAt, &out.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return workflow.ErrNotFound
	}
	if err != nil {
		return err
	}
	return out.Validate()
}

func intervalLiteral(value time.Duration) string {
	return fmt.Sprintf("%d seconds", int(value/time.Second))
}
func sha256Bytes(value []byte) []byte { sum := sha256.Sum256(value); return sum[:] }
func isUTC(value time.Time) bool      { return !value.IsZero() && value.Location() == time.UTC }
func hexDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
func validErrorCode(value string) bool {
	return len(value) >= 1 && len(value) <= 64 && regexp.MustCompile(`^[a-z][a-z0-9._-]*$`).MatchString(value)
}
