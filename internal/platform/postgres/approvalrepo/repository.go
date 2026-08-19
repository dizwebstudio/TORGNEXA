// Package approvalrepo implements TORGNEXA's tenant-scoped PostgreSQL approval engine.
package approvalrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`

type Repository struct{ database *sql.DB }

func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("approval repository: database is required")
	}
	return &Repository{database: database}, nil
}

// InstallPolicy appends a new immutable policy version and retires the prior
// active policy for the same action/resource in one audited transaction.
func (r *Repository) InstallPolicy(ctx context.Context, scope tenancy.Scope, policy approval.Policy, mutation approval.Mutation) error {
	if err := validate(ctx, r, scope); err != nil {
		return err
	}
	if policy.Validate() != nil || mutation.Validate() != nil {
		return approval.ErrInvalid
	}
	if policy.OrganizationID != scope.OrganizationID().String() || policy.WorkspaceID != scope.WorkspaceID().String() || !policy.Active {
		return approval.ErrInvalid
	}
	return r.withTx(ctx, scope, func(tx *sql.Tx) error {
		var maxVersion sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT max(version) FROM approval_policies WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, policy.OrganizationID, policy.WorkspaceID, policy.ID).Scan(&maxVersion); err != nil {
			return fmt.Errorf("approval repository: policy version: %w", err)
		}
		expected := int64(1)
		if maxVersion.Valid {
			expected = maxVersion.Int64 + 1
		}
		if policy.Version != expected {
			return approval.ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE approval_policies SET active=false,retired_at=$5 WHERE organization_id=$1 AND workspace_id=$2 AND action=$3 AND resource_type=$4 AND active`, policy.OrganizationID, policy.WorkspaceID, policy.Action, policy.ResourceType, mutation.OccurredAt); err != nil {
			return fmt.Errorf("approval repository: retire policy: %w", err)
		}
		stages, err := json.Marshal(policy.Stages)
		if err != nil {
			return approval.ErrInvalid
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO approval_policies(id,organization_id,workspace_id,version,name,action,resource_type,minimum_risk,minimum_risk_rank,request_ttl_seconds,escalate_after_seconds,separation_of_duties,stages,active,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,true,$14)`, policy.ID, policy.OrganizationID, policy.WorkspaceID, policy.Version, policy.Name, policy.Action, policy.ResourceType, string(policy.MinimumRisk), riskRank(policy.MinimumRisk), int64(policy.RequestTTL/time.Second), int64(policy.EscalateAfter/time.Second), policy.SeparationOfDuties, string(stages), mutation.OccurredAt)
		if err != nil {
			return fmt.Errorf("approval repository: insert policy: %w", err)
		}
		if err := appendAudit(ctx, tx, scope, mutation, "approval.policy.installed", "approval_policy", policy.ID, audit.RiskLegallySignificant, audit.Summary{"policy_id": policy.ID, "policy_version": policy.Version, "action": policy.Action, "resource_type": policy.ResourceType, "minimum_risk": string(policy.MinimumRisk), "stage_count": len(policy.Stages)}); err != nil {
			return err
		}
		return enqueue(ctx, tx, scope, mutation, "governance.approval.policy_changed.v1", "approval_policy", policy.ID, map[string]any{"policy_id": policy.ID, "policy_version": policy.Version, "action": policy.Action, "resource_type": policy.ResourceType, "minimum_risk": string(policy.MinimumRisk), "stage_count": len(policy.Stages), "change": "installed"})
	})
}

func (r *Repository) ResolvePolicy(ctx context.Context, scope tenancy.Scope, action, resourceType string, risk approval.RiskClass) (approval.Policy, error) {
	if err := validate(ctx, r, scope); err != nil {
		return approval.Policy{}, err
	}
	if !risk.Valid() {
		return approval.Policy{}, approval.ErrInvalid
	}
	var p approval.Policy
	err := r.withReadTx(ctx, scope, func(tx *sql.Tx) error {
		var err error
		p, err = loadActivePolicy(ctx, tx, scope, action, resourceType, risk)
		return err
	})
	return p, err
}

// ListPolicies returns the current policy version for every policy id.
func (r *Repository) ListPolicies(ctx context.Context, scope tenancy.Scope, limit int) ([]approval.Policy, error) {
	if err := validate(ctx, r, scope); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 200 {
		return nil, approval.ErrInvalid
	}
	out := make([]approval.Policy, 0)
	err := r.withReadTx(ctx, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT ON (id) id,version,name,action,resource_type,minimum_risk,request_ttl_seconds,escalate_after_seconds,separation_of_duties,stages,active FROM approval_policies WHERE organization_id=$1 AND workspace_id=$2 ORDER BY id,version DESC LIMIT $3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			policy, err := scanPolicy(rows, scope)
			if err != nil {
				return err
			}
			out = append(out, policy)
		}
		return rows.Err()
	})
	return out, err
}

// RetirePolicy disables one current policy without deleting immutable versions.
func (r *Repository) RetirePolicy(ctx context.Context, scope tenancy.Scope, id string, expectedVersion int64, mutation approval.Mutation) error {
	if err := validate(ctx, r, scope); err != nil {
		return err
	}
	if id == "" || expectedVersion < 1 || mutation.Validate() != nil {
		return approval.ErrInvalid
	}
	return r.withTx(ctx, scope, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE approval_policies SET active=false,retired_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$5 AND active`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id, mutation.OccurredAt, expectedVersion)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return approval.ErrConflict
		}
		if err := appendAudit(ctx, tx, scope, mutation, "approval.policy.retired", "approval_policy", id, audit.RiskLegallySignificant, audit.Summary{"policy_id": id, "policy_version": expectedVersion}); err != nil {
			return err
		}
		return enqueue(ctx, tx, scope, mutation, "governance.approval.policy_changed.v1", "approval_policy", id, map[string]any{"policy_id": id, "policy_version": expectedVersion, "change": "retired"})
	})
}

func (r *Repository) Request(ctx context.Context, scope tenancy.Scope, id string) (approval.Request, error) {
	if err := validate(ctx, r, scope); err != nil {
		return approval.Request{}, err
	}
	var result approval.Request
	err := r.withReadTx(ctx, scope, func(tx *sql.Tx) error {
		var err error
		result, err = loadRequest(ctx, tx, scope, id, false)
		return err
	})
	return result, err
}

// ListRequests returns recent approval requests for the current workspace.
func (r *Repository) ListRequests(ctx context.Context, scope tenancy.Scope, limit int) ([]approval.Request, error) {
	if err := validate(ctx, r, scope); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 200 {
		return nil, approval.ErrInvalid
	}
	out := make([]approval.Request, 0)
	err := r.withReadTx(ctx, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,policy_id,policy_version,requester_id,source,action,resource_type,resource_id,correlation_id,risk,state,current_stage,expires_at,next_escalation_at,escalation_count,version,requested_at,approved_at,rejected_at,execution_started_at,completed_at,failure_code FROM approval_requests WHERE organization_id=$1 AND workspace_id=$2 ORDER BY requested_at DESC,id DESC LIMIT $3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanRequest(rows, scope)
			if err != nil {
				return err
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}

func (r *Repository) CreateRequest(ctx context.Context, scope tenancy.Scope, action, resourceType string, cmd approval.RequestCommand) (approval.Request, error) {
	if err := validate(ctx, r, scope); err != nil {
		return approval.Request{}, err
	}
	if cmd.Validate() != nil {
		return approval.Request{}, approval.ErrInvalid
	}
	var result approval.Request
	err := r.withTx(ctx, scope, func(tx *sql.Tx) error {
		p, err := loadActivePolicy(ctx, tx, scope, action, resourceType, cmd.Risk)
		if err != nil {
			return err
		}
		req, err := approval.NewRequest(p, cmd.RequestID, cmd.Mutation.ActorID, cmd.Mutation.Source, cmd.ResourceID, cmd.Mutation.CorrelationID, cmd.Risk, cmd.Mutation.OccurredAt)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO approval_requests(id,organization_id,workspace_id,policy_id,policy_version,requester_id,source,action,resource_type,resource_id,correlation_id,risk,state,current_stage,expires_at,next_escalation_at,version,requested_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'pending',1,$13,$14,1,$15)`, req.ID, req.OrganizationID, req.WorkspaceID, req.PolicyID, req.PolicyVersion, req.RequesterID, req.Source, req.Action, req.ResourceType, req.ResourceID, req.CorrelationID, string(req.Risk), req.ExpiresAt, req.NextEscalationAt, req.RequestedAt)
		if err != nil {
			return fmt.Errorf("approval repository: insert request: %w", err)
		}
		if err := appendAudit(ctx, tx, scope, cmd.Mutation, "approval.request.requested", "approval_request", req.ID, audit.Risk(req.Risk), audit.Summary{"request_id": req.ID, "policy_id": req.PolicyID, "policy_version": req.PolicyVersion, "action": req.Action, "resource_type": req.ResourceType, "resource_id": req.ResourceID, "risk": string(req.Risk), "current_stage": req.CurrentStage, "expires_at": req.ExpiresAt.Format(time.RFC3339Nano)}); err != nil {
			return err
		}
		if err := enqueue(ctx, tx, scope, cmd.Mutation, "governance.approval.requested.v1", "approval_request", req.ID, requestEvent(req, "requested")); err != nil {
			return err
		}
		result = req
		return nil
	})
	return result, err
}

func (r *Repository) Decide(ctx context.Context, scope tenancy.Scope, cmd approval.DecideCommand) (approval.Request, error) {
	if err := validate(ctx, r, scope); err != nil {
		return approval.Request{}, err
	}
	if cmd.Validate() != nil || cmd.Actor.ID != cmd.Mutation.ActorID {
		return approval.Request{}, approval.ErrInvalid
	}
	var result approval.Request
	err := r.withTx(ctx, scope, func(tx *sql.Tx) error {
		req, err := loadRequest(ctx, tx, scope, cmd.RequestID, true)
		if err != nil {
			return err
		}
		if req.Version != cmd.ExpectedVersion {
			return approval.ErrConflict
		}
		p, err := loadPolicyVersion(ctx, tx, scope, req.PolicyID, req.PolicyVersion)
		if err != nil {
			return err
		}
		prior, err := loadDecisions(ctx, tx, scope, req.ID)
		if err != nil {
			return err
		}
		next, decision, err := approval.ApplyDecision(req, p, prior, cmd.Actor, cmd.DecisionID, cmd.Vote, cmd.Comment, cmd.Mutation.OccurredAt)
		if err != nil {
			return err
		}
		scopesJSON, err := json.Marshal(decision.ActorScopes)
		if err != nil {
			return approval.ErrInvalid
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO approval_decisions(id,organization_id,workspace_id,request_id,stage,actor_id,decision,actor_scopes,comment,decided_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10)`, decision.ID, scope.OrganizationID().String(), scope.WorkspaceID().String(), decision.RequestID, decision.Stage, decision.ActorID, string(decision.Vote), string(scopesJSON), decision.Comment, decision.DecidedAt)
		if err != nil {
			return fmt.Errorf("approval repository: insert decision: %w", err)
		}
		if err := updateRequest(ctx, tx, scope, req.Version, next); err != nil {
			return err
		}
		risk := audit.Risk(req.Risk)
		if err := appendAudit(ctx, tx, scope, cmd.Mutation, "approval.request.decided", "approval_request", req.ID, risk, audit.Summary{"request_id": req.ID, "decision_id": decision.ID, "stage": decision.Stage, "decision": string(decision.Vote), "state": string(next.State), "version": next.Version}); err != nil {
			return err
		}
		if err := enqueue(ctx, tx, scope, cmd.Mutation, "governance.approval.decided.v1", "approval_request", req.ID, map[string]any{"request_id": req.ID, "decision_id": decision.ID, "stage": decision.Stage, "decision": string(decision.Vote), "state": string(next.State), "version": next.Version}); err != nil {
			return err
		}
		result = next
		return nil
	})
	return result, err
}

func (r *Repository) Escalate(ctx context.Context, scope tenancy.Scope, cmd approval.EscalateCommand) (approval.Request, error) {
	if err := validate(ctx, r, scope); err != nil {
		return approval.Request{}, err
	}
	if cmd.Validate() != nil {
		return approval.Request{}, approval.ErrInvalid
	}
	var result approval.Request
	err := r.withTx(ctx, scope, func(tx *sql.Tx) error {
		req, err := loadRequest(ctx, tx, scope, cmd.RequestID, true)
		if err != nil {
			return err
		}
		if req.Version != cmd.ExpectedVersion {
			return approval.ErrConflict
		}
		p, err := loadPolicyVersion(ctx, tx, scope, req.PolicyID, req.PolicyVersion)
		if err != nil {
			return err
		}
		next, err := approval.Escalate(req, p, cmd.Mutation.OccurredAt)
		if err != nil {
			return err
		}
		if err := updateRequest(ctx, tx, scope, req.Version, next); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO approval_escalations(id,organization_id,workspace_id,request_id,stage,escalation_number,escalated_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, cmd.EscalationID, scope.OrganizationID().String(), scope.WorkspaceID().String(), req.ID, req.CurrentStage, next.EscalationCount, cmd.Mutation.OccurredAt)
		if err != nil {
			return fmt.Errorf("approval repository: escalation evidence: %w", err)
		}
		if err := appendAudit(ctx, tx, scope, cmd.Mutation, "approval.request.escalated", "approval_request", req.ID, audit.Risk(req.Risk), audit.Summary{"request_id": req.ID, "stage": req.CurrentStage, "escalation_count": next.EscalationCount, "version": next.Version}); err != nil {
			return err
		}
		if err := enqueue(ctx, tx, scope, cmd.Mutation, "governance.approval.escalated.v1", "approval_request", req.ID, map[string]any{"request_id": req.ID, "stage": req.CurrentStage, "escalation_count": next.EscalationCount, "state": string(next.State), "version": next.Version}); err != nil {
			return err
		}
		result = next
		return nil
	})
	return result, err
}

func (r *Repository) Expire(ctx context.Context, scope tenancy.Scope, cmd approval.TransitionCommand) (approval.Request, error) {
	return r.transition(ctx, scope, cmd, "expire")
}
func (r *Repository) BeginExecution(ctx context.Context, scope tenancy.Scope, cmd approval.TransitionCommand) (approval.Request, error) {
	return r.transition(ctx, scope, cmd, "begin_execution")
}
func (r *Repository) CompleteExecution(ctx context.Context, scope tenancy.Scope, cmd approval.TransitionCommand, success bool) (approval.Request, error) {
	if success && cmd.FailureCode != "" {
		return approval.Request{}, approval.ErrInvalid
	}
	if !success && cmd.FailureCode == "" {
		return approval.Request{}, approval.ErrInvalid
	}
	if success {
		return r.transition(ctx, scope, cmd, "complete")
	}
	return r.transition(ctx, scope, cmd, "fail")
}

func (r *Repository) transition(ctx context.Context, scope tenancy.Scope, cmd approval.TransitionCommand, op string) (approval.Request, error) {
	if err := validate(ctx, r, scope); err != nil {
		return approval.Request{}, err
	}
	if cmd.Validate() != nil {
		return approval.Request{}, approval.ErrInvalid
	}
	var result approval.Request
	err := r.withTx(ctx, scope, func(tx *sql.Tx) error {
		req, err := loadRequest(ctx, tx, scope, cmd.RequestID, true)
		if err != nil {
			return err
		}
		if req.Version != cmd.ExpectedVersion {
			return approval.ErrConflict
		}
		var next approval.Request
		switch op {
		case "expire":
			next, err = approval.Expire(req, cmd.Mutation.OccurredAt)
		case "begin_execution":
			next, err = approval.BeginExecution(req, cmd.Mutation.OccurredAt)
		case "complete":
			next, err = approval.CompleteExecution(req, true, "", cmd.Mutation.OccurredAt)
		case "fail":
			next, err = approval.CompleteExecution(req, false, cmd.FailureCode, cmd.Mutation.OccurredAt)
		default:
			err = approval.ErrInvalid
		}
		if err != nil {
			return err
		}
		if err := updateRequest(ctx, tx, scope, req.Version, next); err != nil {
			return err
		}
		action := "approval.request." + op
		if err := appendAudit(ctx, tx, scope, cmd.Mutation, action, "approval_request", req.ID, audit.Risk(req.Risk), audit.Summary{"request_id": req.ID, "old_state": string(req.State), "state": string(next.State), "version": next.Version, "failure_code": next.FailureCode}); err != nil {
			return err
		}
		if err := enqueue(ctx, tx, scope, cmd.Mutation, "governance.approval.state_changed.v1", "approval_request", req.ID, requestEvent(next, op)); err != nil {
			return err
		}
		result = next
		return nil
	})
	return result, err
}

// Due returns pending/approved requests requiring expiry or escalation handling.
func (r *Repository) Due(ctx context.Context, scope tenancy.Scope, now time.Time, limit int) ([]approval.Request, error) {
	if err := validate(ctx, r, scope); err != nil {
		return nil, err
	}
	if now.IsZero() || limit < 1 || limit > 1000 {
		return nil, approval.ErrInvalid
	}
	now = now.UTC()
	out := []approval.Request{}
	err := r.withReadTx(ctx, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,policy_id,policy_version,requester_id,source,action,resource_type,resource_id,correlation_id,risk,state,current_stage,expires_at,next_escalation_at,escalation_count,version,requested_at,approved_at,rejected_at,execution_started_at,completed_at,failure_code FROM approval_requests WHERE organization_id=$1 AND workspace_id=$2 AND ((state='pending' AND (expires_at<=$3 OR (next_escalation_at IS NOT NULL AND next_escalation_at<=$3))) OR (state='approved' AND expires_at<=$3)) ORDER BY LEAST(expires_at,COALESCE(next_escalation_at,expires_at)),id LIMIT $4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), now, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			q, err := scanRequest(rows, scope)
			if err != nil {
				return err
			}
			out = append(out, q)
		}
		return rows.Err()
	})
	return out, err
}

func validate(ctx context.Context, r *Repository, scope tenancy.Scope) error {
	if ctx == nil {
		return errors.New("approval repository: context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.database == nil {
		return errors.New("approval repository: uninitialized")
	}
	if !scope.Valid() {
		return tenancy.ErrInvalidScope
	}
	return nil
}
func (r *Repository) withTx(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	return r.tx(ctx, scope, false, fn)
}
func (r *Repository) withReadTx(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	return r.tx(ctx, scope, true, fn)
}
func (r *Repository) tx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var o, w string
	if err := tx.QueryRowContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&o, &w); err != nil {
		return err
	}
	if o != scope.OrganizationID().String() || w != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func loadActivePolicy(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, action, resourceType string, risk approval.RiskClass) (approval.Policy, error) {
	row := tx.QueryRowContext(ctx, `SELECT id,version,name,action,resource_type,minimum_risk,request_ttl_seconds,escalate_after_seconds,separation_of_duties,stages,active FROM approval_policies WHERE organization_id=$1 AND workspace_id=$2 AND action=$3 AND resource_type=$4 AND active AND minimum_risk_rank<=$5 ORDER BY minimum_risk_rank DESC,version DESC LIMIT 1`, scope.OrganizationID().String(), scope.WorkspaceID().String(), action, resourceType, riskRank(risk))
	return scanPolicy(row, scope)
}
func loadPolicyVersion(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, id string, version int64) (approval.Policy, error) {
	return scanPolicy(tx.QueryRowContext(ctx, `SELECT id,version,name,action,resource_type,minimum_risk,request_ttl_seconds,escalate_after_seconds,separation_of_duties,stages,active FROM approval_policies WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id, version), scope)
}

type scanner interface{ Scan(...any) error }

func scanPolicy(row scanner, scope tenancy.Scope) (approval.Policy, error) {
	var p approval.Policy
	var risk string
	var ttl, esc int64
	var raw []byte
	if err := row.Scan(&p.ID, &p.Version, &p.Name, &p.Action, &p.ResourceType, &risk, &ttl, &esc, &p.SeparationOfDuties, &raw, &p.Active); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return approval.Policy{}, approval.ErrDenied
		}
		return approval.Policy{}, err
	}
	p.OrganizationID = scope.OrganizationID().String()
	p.WorkspaceID = scope.WorkspaceID().String()
	p.MinimumRisk = approval.RiskClass(risk)
	p.RequestTTL = time.Duration(ttl) * time.Second
	p.EscalateAfter = time.Duration(esc) * time.Second
	if err := json.Unmarshal(raw, &p.Stages); err != nil || p.Validate() != nil {
		return approval.Policy{}, approval.ErrInvalid
	}
	return p, nil
}

func loadRequest(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, id string, lock bool) (approval.Request, error) {
	q := `SELECT id,policy_id,policy_version,requester_id,source,action,resource_type,resource_id,correlation_id,risk,state,current_stage,expires_at,next_escalation_at,escalation_count,version,requested_at,approved_at,rejected_at,execution_started_at,completed_at,failure_code FROM approval_requests WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`
	if lock {
		q += " FOR UPDATE"
	}
	return scanRequest(tx.QueryRowContext(ctx, q, scope.OrganizationID().String(), scope.WorkspaceID().String(), id), scope)
}
func scanRequest(row scanner, scope tenancy.Scope) (approval.Request, error) {
	var r approval.Request
	var risk, state string
	var next, approved, rejected, started, completed sql.NullTime
	var failure sql.NullString
	if err := row.Scan(&r.ID, &r.PolicyID, &r.PolicyVersion, &r.RequesterID, &r.Source, &r.Action, &r.ResourceType, &r.ResourceID, &r.CorrelationID, &risk, &state, &r.CurrentStage, &r.ExpiresAt, &next, &r.EscalationCount, &r.Version, &r.RequestedAt, &approved, &rejected, &started, &completed, &failure); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return approval.Request{}, approval.ErrInvalid
		}
		return approval.Request{}, err
	}
	r.OrganizationID = scope.OrganizationID().String()
	r.WorkspaceID = scope.WorkspaceID().String()
	r.Risk = approval.RiskClass(risk)
	r.State = approval.RequestState(state)
	r.RequestedAt = r.RequestedAt.UTC()
	r.ExpiresAt = r.ExpiresAt.UTC()
	if next.Valid {
		t := next.Time.UTC()
		r.NextEscalationAt = &t
	}
	if approved.Valid {
		t := approved.Time.UTC()
		r.ApprovedAt = &t
	}
	if rejected.Valid {
		t := rejected.Time.UTC()
		r.RejectedAt = &t
	}
	if started.Valid {
		t := started.Time.UTC()
		r.ExecutionStartedAt = &t
	}
	if completed.Valid {
		t := completed.Time.UTC()
		r.CompletedAt = &t
	}
	if failure.Valid {
		r.FailureCode = failure.String
	}
	if r.Validate() != nil {
		return approval.Request{}, approval.ErrInvalid
	}
	return r, nil
}
func loadDecisions(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, requestID string) ([]approval.DecisionRecord, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,request_id,stage,actor_id,decision,actor_scopes,comment,decided_at FROM approval_decisions WHERE organization_id=$1 AND workspace_id=$2 AND request_id=$3 ORDER BY stage,decided_at,id`, scope.OrganizationID().String(), scope.WorkspaceID().String(), requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []approval.DecisionRecord{}
	for rows.Next() {
		var d approval.DecisionRecord
		var v string
		var scopesRaw []byte
		if err := rows.Scan(&d.ID, &d.RequestID, &d.Stage, &d.ActorID, &v, &scopesRaw, &d.Comment, &d.DecidedAt); err != nil {
			return nil, err
		}
		d.Vote = approval.Vote(v)
		if err := json.Unmarshal(scopesRaw, &d.ActorScopes); err != nil {
			return nil, approval.ErrInvalid
		}
		d.DecidedAt = d.DecidedAt.UTC()
		if d.Validate() != nil {
			return nil, approval.ErrInvalid
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func updateRequest(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, expected int64, r approval.Request) error {
	res, err := tx.ExecContext(ctx, `UPDATE approval_requests SET state=$4,current_stage=$5,next_escalation_at=$6,escalation_count=$7,version=$8,approved_at=$9,rejected_at=$10,execution_started_at=$11,completed_at=$12,failure_code=NULLIF($13,'') WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$14`, scope.OrganizationID().String(), scope.WorkspaceID().String(), r.ID, string(r.State), r.CurrentStage, r.NextEscalationAt, r.EscalationCount, r.Version, r.ApprovedAt, r.RejectedAt, r.ExecutionStartedAt, r.CompletedAt, r.FailureCode, expected)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return approval.ErrConflict
	}
	return nil
}

func appendAudit(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, m approval.Mutation, action, resourceType, resourceID string, risk audit.Risk, summary audit.Summary) error {
	safe, err := audit.SanitizeSummary(summary)
	if err != nil {
		return err
	}
	rec := audit.Record{ID: m.AuditID, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), ActorID: m.ActorID, Source: m.Source, Action: action, ResourceType: resourceType, ResourceID: resourceID, CorrelationID: m.CorrelationID, Risk: risk, Summary: safe, CreatedAt: m.OccurredAt}
	return auditrepo.AppendTransaction(ctx, tx, scope, rec)
}
func enqueue(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, m approval.Mutation, typeValue, entityType, entityID string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	typ, err := eventbus.ParseEventType(typeValue)
	if err != nil {
		return err
	}
	at, err := domain.NewUTCInstant(m.OccurredAt)
	if err != nil {
		return err
	}
	ev := eventbus.Event{ID: m.EventID, Type: typ, OccurredAt: at, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), EntityType: entityType, EntityID: entityID, Source: m.Source, CorrelationID: m.CorrelationID, CausationID: m.CausationID, ActorID: m.ActorID, TraceID: m.TraceID, Data: data}
	if err := ev.Validate(); err != nil {
		return err
	}
	enq, err := outboxrepo.NewTransactionEnqueuer(tx)
	if err != nil {
		return err
	}
	return enq.Enqueue(ctx, ev)
}
func requestEvent(r approval.Request, change string) map[string]any {
	return map[string]any{"request_id": r.ID, "policy_id": r.PolicyID, "policy_version": r.PolicyVersion, "action": r.Action, "resource_type": r.ResourceType, "resource_id": r.ResourceID, "risk": string(r.Risk), "state": string(r.State), "current_stage": r.CurrentStage, "version": r.Version, "change": change}
}
func riskRank(r approval.RiskClass) int {
	switch r {
	case approval.RiskRead:
		return 1
	case approval.RiskWriteSafe:
		return 2
	case approval.RiskWriteSensitive:
		return 3
	case approval.RiskLegallySignificant:
		return 4
	}
	return 0
}
