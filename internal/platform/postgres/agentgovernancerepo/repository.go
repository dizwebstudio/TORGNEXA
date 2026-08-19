// Package agentgovernancerepo implements durable tenant-scoped AI agent governance state.
package agentgovernancerepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/agentgovernance"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

type Repository struct{ database *sql.DB }

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("agent governance repository: database required")
	}
	return &Repository{database: db}, nil
}

func (r *Repository) ResolveAgentPolicy(ctx context.Context, scope tenancy.Scope, agent agentgovernance.Agent, at time.Time) (agentgovernance.Policy, error) {
	if err := validate(r, ctx, scope); err != nil || !agent.Valid() || at.IsZero() || at.Location() != time.UTC {
		return agentgovernance.Policy{}, agentgovernance.ErrInvalid
	}
	var out agentgovernance.Policy
	err := r.withReadTx(ctx, scope, func(tx *sql.Tx) error {
		var raw []byte
		var until sql.NullTime
		err := tx.QueryRowContext(ctx, `SELECT id,version,agent_id,integration_id,rules,effective_from,effective_until
FROM ai_agent_policies
WHERE organization_id=$1 AND workspace_id=$2 AND agent_id=$3 AND integration_id=$4
  AND effective_from<=$5 AND (effective_until IS NULL OR effective_until>$5)
ORDER BY version DESC LIMIT 1`, scope.OrganizationID().String(), scope.WorkspaceID().String(), agent.ID, agent.IntegrationID, at).Scan(&out.ID, &out.Version, &out.AgentID, &out.IntegrationID, &raw, &out.EffectiveFrom, &until)
		if errors.Is(err, sql.ErrNoRows) {
			return agentgovernance.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("agent governance repository: resolve policy: %w", err)
		}
		if err := json.Unmarshal(raw, &out.Rules); err != nil {
			return agentgovernance.ErrInvalid
		}
		if until.Valid {
			v := until.Time.UTC()
			out.EffectiveUntil = &v
		}
		out.EffectiveFrom = out.EffectiveFrom.UTC()
		canonical, err := agentgovernance.CanonicalRules(out.Rules)
		if err != nil {
			return err
		}
		out.Rules = canonical
		return out.Validate()
	})
	return out, err
}

// InstallPolicy appends one immutable policy version through a trusted control plane.
// Database guards enforce monotonic versions and a stable policy id per agent/integration.
func (r *Repository) InstallPolicy(ctx context.Context, scope tenancy.Scope, policy agentgovernance.Policy, change agentgovernance.Change) error {
	if err := validate(r, ctx, scope); err != nil || policy.Validate() != nil || change.Validate() != nil {
		return agentgovernance.ErrInvalid
	}
	canonical, err := agentgovernance.CanonicalRules(policy.Rules)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return agentgovernance.ErrInvalid
	}
	var until any
	if policy.EffectiveUntil != nil {
		until = policy.EffectiveUntil.UTC()
	}
	return r.withTx(ctx, scope, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO ai_agent_policies(id,organization_id,workspace_id,version,agent_id,integration_id,rules,effective_from,effective_until,changed_by,reason,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, policy.ID, scope.OrganizationID().String(), scope.WorkspaceID().String(), policy.Version, policy.AgentID, policy.IntegrationID, raw, policy.EffectiveFrom, until, change.ActorID, change.Reason, change.OccurredAt)
		if err != nil {
			return fmt.Errorf("agent governance repository: install policy: %w", err)
		}
		return nil
	})
}

// RecordKillSwitch appends a versioned tenant/agent/integration operational state.
// Re-enabling is another immutable version; evidence is never updated in place.
func (r *Repository) RecordKillSwitch(ctx context.Context, scope tenancy.Scope, change agentgovernance.KillChange) error {
	if err := validate(r, ctx, scope); err != nil || change.Validate() != nil {
		return agentgovernance.ErrInvalid
	}
	return r.withTx(ctx, scope, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO ai_agent_kill_switches(organization_id,workspace_id,scope_kind,subject_id,version,disabled,changed_by,reason,changed_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), string(change.Scope), change.SubjectID, change.Version, change.Disabled, change.Change.ActorID, change.Change.Reason, change.Change.OccurredAt)
		if err != nil {
			return fmt.Errorf("agent governance repository: record kill switch: %w", err)
		}
		return nil
	})
}

func (r *Repository) AgentKillState(ctx context.Context, scope tenancy.Scope, agent agentgovernance.Agent) (agentgovernance.KillState, error) {
	if err := validate(r, ctx, scope); err != nil || !agent.Valid() {
		return agentgovernance.KillState{}, agentgovernance.ErrInvalid
	}
	state := agentgovernance.KillState{}
	err := r.withReadTx(ctx, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT ON (scope_kind,subject_id) scope_kind,disabled,version
FROM ai_agent_kill_switches
WHERE organization_id=$1 AND workspace_id=$2 AND (
  (scope_kind='tenant' AND subject_id='*') OR
  (scope_kind='agent' AND subject_id=$3) OR
  (scope_kind='integration' AND subject_id=$4)
)
ORDER BY scope_kind,subject_id,version DESC`, scope.OrganizationID().String(), scope.WorkspaceID().String(), agent.ID, agent.IntegrationID)
		if err != nil {
			return fmt.Errorf("agent governance repository: kill state: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var kind string
			var disabled bool
			var version uint64
			if err := rows.Scan(&kind, &disabled, &version); err != nil {
				return err
			}
			if version > state.Version {
				state.Version = version
			}
			switch kind {
			case "tenant":
				state.TenantDisabled = disabled
			case "agent":
				state.AgentDisabled = disabled
			case "integration":
				state.IntegrationDisabled = disabled
			default:
				return agentgovernance.ErrInvalid
			}
		}
		return rows.Err()
	})
	return state, err
}

// ConsumeAgentCall atomically records one invocation and increments the bounded
// window only when the call is allowed. Replaying the same invocation id returns
// the original decision without consuming the counter twice.
func (r *Repository) ConsumeAgentCall(ctx context.Context, scope tenancy.Scope, req agentgovernance.FrequencyRequest) (bool, error) {
	if err := validate(r, ctx, scope); err != nil || req.Validate() != nil || req.PolicyVersion > math.MaxInt64 {
		return false, agentgovernance.ErrInvalid
	}
	allowed := false
	err := r.withTx(ctx, scope, func(tx *sql.Tx) error {
		windowKey := fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%s\x00%s", scope.OrganizationID().String(), scope.WorkspaceID().String(), req.PolicyID, req.PolicyVersion, req.AgentID, req.IntegrationID, req.Tool, req.WindowStart.Format(time.RFC3339Nano))
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, windowKey); err != nil {
			return fmt.Errorf("agent governance repository: usage lock: %w", err)
		}

		var existingAllowed bool
		var policyID, agentID, integrationID, tool string
		var policyVersion uint64
		var windowStart, windowEnd, occurredAt time.Time
		var maxCalls int64
		err := tx.QueryRowContext(ctx, `SELECT policy_id,policy_version,agent_id,integration_id,tool,window_start,window_end,max_calls_snapshot,allowed,occurred_at
FROM ai_agent_call_usage
WHERE organization_id=$1 AND workspace_id=$2 AND invocation_id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), req.InvocationID).Scan(&policyID, &policyVersion, &agentID, &integrationID, &tool, &windowStart, &windowEnd, &maxCalls, &existingAllowed, &occurredAt)
		if err == nil {
			if policyID != req.PolicyID || policyVersion != req.PolicyVersion || agentID != req.AgentID || integrationID != req.IntegrationID || tool != req.Tool || !windowStart.Equal(req.WindowStart) || !windowEnd.Equal(req.WindowEnd) || maxCalls != req.MaxCalls || !occurredAt.Equal(req.OccurredAt) {
				return agentgovernance.ErrConflict
			}
			allowed = existingAllowed
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("agent governance repository: usage replay: %w", err)
		}

		var used, snapshot int64
		var existingEnd time.Time
		err = tx.QueryRowContext(ctx, `SELECT used,max_calls_snapshot,window_end FROM ai_agent_call_counters
WHERE organization_id=$1 AND workspace_id=$2 AND policy_id=$3 AND policy_version=$4 AND agent_id=$5 AND integration_id=$6 AND tool=$7 AND window_start=$8
FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), req.PolicyID, req.PolicyVersion, req.AgentID, req.IntegrationID, req.Tool, req.WindowStart).Scan(&used, &snapshot, &existingEnd)
		if errors.Is(err, sql.ErrNoRows) {
			if _, err := tx.ExecContext(ctx, `INSERT INTO ai_agent_call_counters(organization_id,workspace_id,policy_id,policy_version,agent_id,integration_id,tool,window_start,window_end,used,max_calls_snapshot,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,0,$10,$11)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), req.PolicyID, req.PolicyVersion, req.AgentID, req.IntegrationID, req.Tool, req.WindowStart, req.WindowEnd, req.MaxCalls, req.OccurredAt); err != nil {
				return fmt.Errorf("agent governance repository: counter insert: %w", err)
			}
			used, snapshot, existingEnd = 0, req.MaxCalls, req.WindowEnd
		} else if err != nil {
			return fmt.Errorf("agent governance repository: counter read: %w", err)
		}
		if snapshot != req.MaxCalls || !existingEnd.Equal(req.WindowEnd) {
			return agentgovernance.ErrConflict
		}
		allowed = used < req.MaxCalls
		if _, err := tx.ExecContext(ctx, `INSERT INTO ai_agent_call_usage(invocation_id,organization_id,workspace_id,policy_id,policy_version,agent_id,integration_id,tool,window_start,window_end,max_calls_snapshot,allowed,occurred_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, req.InvocationID, scope.OrganizationID().String(), scope.WorkspaceID().String(), req.PolicyID, req.PolicyVersion, req.AgentID, req.IntegrationID, req.Tool, req.WindowStart, req.WindowEnd, req.MaxCalls, allowed, req.OccurredAt); err != nil {
			return fmt.Errorf("agent governance repository: usage insert: %w", err)
		}
		if allowed {
			result, err := tx.ExecContext(ctx, `UPDATE ai_agent_call_counters SET used=used+1,updated_at=$9
WHERE organization_id=$1 AND workspace_id=$2 AND policy_id=$3 AND policy_version=$4 AND agent_id=$5 AND integration_id=$6 AND tool=$7 AND window_start=$8 AND used<max_calls_snapshot`, scope.OrganizationID().String(), scope.WorkspaceID().String(), req.PolicyID, req.PolicyVersion, req.AgentID, req.IntegrationID, req.Tool, req.WindowStart, req.OccurredAt)
			if err != nil {
				return fmt.Errorf("agent governance repository: counter update: %w", err)
			}
			rows, err := result.RowsAffected()
			if err != nil || rows != 1 {
				return agentgovernance.ErrConflict
			}
		}
		return nil
	})
	return allowed, err
}

func validate(r *Repository, ctx context.Context, scope tenancy.Scope) error {
	if r == nil || r.database == nil || ctx == nil || !scope.Valid() {
		return agentgovernance.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (r *Repository) withTx(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) withReadTx(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
