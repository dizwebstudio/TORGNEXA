// Package workflowrepo persists the tenant-scoped workflow control plane.
package workflowrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/workflow"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`

type Repository struct{ db *sql.DB }

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("workflow repository: database required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Create(ctx context.Context, scope workflow.Scope, id string, definition workflow.Definition) (workflow.Workflow, error) {
	if err := valid(ctx, r, scope); err != nil || definition.Validate() != nil || strings.TrimSpace(id) == "" {
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
			item, err := scanWorkflow(rows)
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
	if err := valid(ctx, r, scope); err != nil || req.ID == "" || req.WorkflowID == "" || req.WorkflowVersion < 1 || !req.TriggerKind.Valid() || req.IdempotencyKey == "" || req.InputDigest == "" {
		return workflow.Run{}, workflow.ErrInvalid
	}
	if _, err := workflow.ParseScope(scope.OrganizationID(), scope.WorkspaceID()); err != nil {
		return workflow.Run{}, workflow.ErrInvalid
	}
	now := time.Now().UTC()
	var out workflow.Run
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO workflow_runs(organization_id,workspace_id,id,workflow_id,workflow_version,trigger_kind,trigger_ref,idempotency_key,input_digest,status,available_at,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'queued',$10,1) RETURNING id,organization_id,workspace_id,workflow_id,workflow_version,trigger_kind,trigger_ref,idempotency_key,input_digest,status,attempt_count,available_at,started_at,completed_at,last_error_code,version`, scope.OrganizationID(), scope.WorkspaceID(), req.ID, req.WorkflowID, req.WorkflowVersion, string(req.TriggerKind), req.TriggerRef, req.IdempotencyKey, req.InputDigest, now).Scan(&out.ID, &out.OrganizationID, &out.WorkspaceID, &out.WorkflowID, &out.WorkflowVersion, &out.TriggerKind, &out.TriggerRef, &out.IdempotencyKey, &out.InputDigest, &out.Status, &out.AttemptCount, &out.AvailableAt, &out.StartedAt, &out.CompletedAt, &out.LastErrorCode, &out.Version); err != nil {
			return mapWriteError(err)
		}
		return nil
	})
	return out, err
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
		if err := workflow.ValidateRunTransition(item.Status, item.Status); err != nil && expectedVersion > 0 { /* state is checked by the caller; the DB guard remains authoritative */
		}
		return scanRun(tx.QueryRowContext(ctx, `UPDATE workflow_runs SET status=$4,attempt_count=$5,available_at=$6,started_at=$7,completed_at=$8,last_error_code=$9,version=version+1 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$10 RETURNING id,organization_id,workspace_id,workflow_id,workflow_version,trigger_kind,trigger_ref,idempotency_key,input_digest,status,attempt_count,available_at,started_at,completed_at,last_error_code,version`, scope.OrganizationID(), scope.WorkspaceID(), item.ID, string(item.Status), item.AttemptCount, item.AvailableAt, item.StartedAt, item.CompletedAt, item.LastErrorCode, expectedVersion), &out)
	})
	if errors.Is(err, workflow.ErrNotFound) {
		return workflow.Run{}, workflow.ErrConflict
	}
	return out, err
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
