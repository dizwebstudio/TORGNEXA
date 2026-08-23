// Package trustcontrolrepo persists tenant-scoped receipts, evidence and
// decision-lab snapshots.
package trustcontrolrepo

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/trustcontrol"
)

var (
	ErrInvalid  = errors.New("trustcontrolrepo: invalid")
	ErrConflict = errors.New("trustcontrolrepo: idempotency conflict")
	ErrNotFound = errors.New("trustcontrolrepo: not found")
)

type Repository struct{ db *sql.DB }

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &Repository{db: db}, nil
}

func (r *Repository) withTx(ctx context.Context, readOnly bool, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	if ctx == nil || !scope.Valid() || r == nil || r.db == nil {
		return ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var org, workspace string
	if err := tx.QueryRowContext(ctx, `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &workspace); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplayRun is one immutable connector replay record built from synthetic data.
type ReplayRun struct {
	ID, ConnectorFamily, Capability, Status, ActorRef string
	FixtureSHA256                                     []byte
	Fixture, Result                                   json.RawMessage
	CreatedAt                                         time.Time
}

// Scenario is one immutable profitability input/result snapshot.
type Scenario struct {
	ID, Name, AlgorithmVersion, ActorRef string
	Input, Result                        json.RawMessage
	InputSHA256                          []byte
	CreatedAt                            time.Time
}

// ReserveExternal claims an idempotency key before an irreversible external
// call. Existing keys never execute again; digest drift is a conflict.
func (r *Repository) ReserveExternal(ctx context.Context, scope tenancy.Scope, operation, key string, digest []byte) (bool, error) {
	claimed := false
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		_, fresh, err := beginReceipt(ctx, tx, scope, operation, key, digest)
		if err != nil {
			return err
		}
		claimed = fresh
		return nil
	})
	return claimed, err
}

// CompleteExternal marks a reserved external operation terminal without
// persisting its request or response body.
func (r *Repository) CompleteExternal(ctx context.Context, scope tenancy.Scope, operation, key, resourceType, resourceID, outcome string) error {
	return r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		return completeReceipt(ctx, tx, scope, operation, key, resourceType, resourceID, map[string]any{"outcome": outcome})
	})
}

func beginReceipt(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, operation, key string, digest []byte) (trustcontrol.Receipt, bool, error) {
	if operation == "" || key == "" || len(key) > 128 || len(digest) != 32 {
		return trustcontrol.Receipt{}, false, ErrInvalid
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO operation_receipts(organization_id,workspace_id,operation,idempotency_key,request_sha256,state) VALUES($1,$2,$3,$4,$5,'pending') ON CONFLICT DO NOTHING`, scope.OrganizationID().String(), scope.WorkspaceID().String(), operation, key, digest)
	if err != nil {
		return trustcontrol.Receipt{}, false, err
	}
	affected, _ := result.RowsAffected()
	if affected == 1 {
		return trustcontrol.Receipt{Operation: operation, Key: key, State: "pending", RequestSHA256: digest}, true, nil
	}
	var receipt trustcontrol.Receipt
	var resultJSON []byte
	err = tx.QueryRowContext(ctx, `SELECT operation,idempotency_key,request_sha256,state,resource_type,resource_id,result,created_at,completed_at FROM operation_receipts WHERE organization_id=$1 AND workspace_id=$2 AND operation=$3 AND idempotency_key=$4 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), operation, key).Scan(&receipt.Operation, &receipt.Key, &receipt.RequestSHA256, &receipt.State, &receipt.ResourceType, &receipt.ResourceID, &resultJSON, &receipt.CreatedAt, &receipt.CompletedAt)
	if err != nil {
		return trustcontrol.Receipt{}, false, err
	}
	if !bytes.Equal(receipt.RequestSHA256, digest) {
		return trustcontrol.Receipt{}, false, ErrConflict
	}
	if err := json.Unmarshal(resultJSON, &receipt.Result); err != nil {
		return trustcontrol.Receipt{}, false, err
	}
	return receipt, false, nil
}

func completeReceipt(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, operation, key, resourceType, resourceID string, result any) error {
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 8192 {
		return ErrInvalid
	}
	changed, err := tx.ExecContext(ctx, `UPDATE operation_receipts SET state='completed',resource_type=$5,resource_id=$6,result=$7,completed_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND operation=$3 AND idempotency_key=$4 AND state='pending'`, scope.OrganizationID().String(), scope.WorkspaceID().String(), operation, key, resourceType, resourceID, encoded)
	if err != nil {
		return err
	}
	rows, _ := changed.RowsAffected()
	if rows != 1 {
		return ErrConflict
	}
	return nil
}

func insertEvidence(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, evidence trustcontrol.Evidence) error {
	if evidence.ID == "" || evidence.Type == "" || evidence.ActorRef == "" || evidence.ResourceType == "" || evidence.ResourceID == "" || evidence.CorrelationID == "" || evidence.Decision == "" {
		return ErrInvalid
	}
	summary, err := json.Marshal(evidence.Summary)
	if err != nil || len(summary) > 8192 {
		return ErrInvalid
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO security_evidence(id,organization_id,workspace_id,evidence_type,actor_ref,resource_type,resource_id,correlation_id,decision,request_sha256,summary,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, evidence.ID, scope.OrganizationID().String(), scope.WorkspaceID().String(), evidence.Type, evidence.ActorRef, evidence.ResourceType, evidence.ResourceID, evidence.CorrelationID, evidence.Decision, nullableDigest(evidence.RequestSHA256), summary, evidence.OccurredAt.UTC())
	return err
}

// ListEvidence returns a cursor page without prompt, response or credential bodies.
func (r *Repository) ListEvidence(ctx context.Context, scope tenancy.Scope, limit int, before time.Time) ([]trustcontrol.Evidence, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	if before.IsZero() {
		before = time.Now().UTC().Add(time.Second)
	}
	var out []trustcontrol.Evidence
	err := r.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,evidence_type,actor_ref,resource_type,resource_id,correlation_id,decision,request_sha256,summary,occurred_at FROM security_evidence WHERE organization_id=$1 AND workspace_id=$2 AND occurred_at<$3 ORDER BY occurred_at DESC,id DESC LIMIT $4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), before.UTC(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item trustcontrol.Evidence
			var digest, summary []byte
			if err := rows.Scan(&item.ID, &item.Type, &item.ActorRef, &item.ResourceType, &item.ResourceID, &item.CorrelationID, &item.Decision, &digest, &summary, &item.OccurredAt); err != nil {
				return err
			}
			item.RequestSHA256 = digest
			if err := json.Unmarshal(summary, &item.Summary); err != nil {
				return err
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}

// CurrentPolicy returns the newest immutable AI egress policy revision.
func (r *Repository) CurrentPolicy(ctx context.Context, scope tenancy.Scope) (trustcontrol.Policy, error) {
	var policy trustcontrol.Policy
	err := r.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		return scanPolicy(tx.QueryRowContext(ctx, `SELECT version,enabled,allowed_data_classes,allowed_providers,allowed_models,max_prompt_bytes,monthly_request_limit,created_at FROM ai_egress_policy_revisions WHERE organization_id=$1 AND workspace_id=$2 ORDER BY version DESC LIMIT 1`, scope.OrganizationID().String(), scope.WorkspaceID().String()), &policy)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return trustcontrol.Policy{}, ErrNotFound
	}
	return policy, err
}

// PutPolicy appends a policy revision, evidence and idempotency receipt atomically.
func (r *Repository) PutPolicy(ctx context.Context, scope tenancy.Scope, id, actor, key string, expected int64, policy trustcontrol.Policy, digest []byte) (trustcontrol.Policy, bool, error) {
	policy.Version = expected + 1
	if expected < 0 || trustcontrol.ValidatePolicy(policy) != nil {
		return trustcontrol.Policy{}, false, ErrInvalid
	}
	replayed := false
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		receipt, claimed, err := beginReceipt(ctx, tx, scope, "ai_egress_policy.put", key, digest)
		if err != nil {
			return err
		}
		if !claimed {
			if receipt.State != "completed" {
				return ErrConflict
			}
			replayed = true
			return scanPolicy(tx.QueryRowContext(ctx, `SELECT version,enabled,allowed_data_classes,allowed_providers,allowed_models,max_prompt_bytes,monthly_request_limit,created_at FROM ai_egress_policy_revisions WHERE organization_id=$1 AND workspace_id=$2 AND version=$3::bigint`, scope.OrganizationID().String(), scope.WorkspaceID().String(), receipt.ResourceID), &policy)
		}
		if err := lockAIPolicy(ctx, tx, scope); err != nil {
			return err
		}
		var current int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(version),0) FROM ai_egress_policy_revisions WHERE organization_id=$1 AND workspace_id=$2`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&current); err != nil || current != expected {
			return ErrConflict
		}
		classes, _ := json.Marshal(policy.AllowedDataClasses)
		destinations, _ := json.Marshal(policy.AllowedDestinations)
		models, _ := json.Marshal(policy.AllowedModels)
		if err := scanPolicy(tx.QueryRowContext(ctx, `INSERT INTO ai_egress_policy_revisions(organization_id,workspace_id,version,enabled,allowed_data_classes,allowed_providers,allowed_models,max_prompt_bytes,monthly_request_limit,actor_ref,correlation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING version,enabled,allowed_data_classes,allowed_providers,allowed_models,max_prompt_bytes,monthly_request_limit,created_at`, scope.OrganizationID().String(), scope.WorkspaceID().String(), policy.Version, policy.Enabled, classes, destinations, models, policy.MaxPromptBytes, policy.MonthlyRequestLimit, actor, key), &policy); err != nil {
			return err
		}
		if err := insertEvidence(ctx, tx, scope, trustcontrol.Evidence{ID: id, Type: "ai.egress.policy", ActorRef: actor, ResourceType: "ai_egress_policy", ResourceID: fmt.Sprint(policy.Version), CorrelationID: key, Decision: "succeeded", RequestSHA256: digest, Summary: map[string]any{"version": policy.Version}, OccurredAt: time.Now().UTC()}); err != nil {
			return err
		}
		return completeReceipt(ctx, tx, scope, "ai_egress_policy.put", key, "ai_egress_policy", fmt.Sprint(policy.Version), map[string]any{"version": policy.Version})
	})
	return policy, replayed, err
}

// EvaluateAI checks the current policy and monthly authorization count.
func (r *Repository) EvaluateAI(ctx context.Context, scope tenancy.Scope, request trustcontrol.EgressRequest) (trustcontrol.Policy, error) {
	var policy trustcontrol.Policy
	err := r.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		if err := scanPolicy(tx.QueryRowContext(ctx, `SELECT version,enabled,allowed_data_classes,allowed_providers,allowed_models,max_prompt_bytes,monthly_request_limit,created_at FROM ai_egress_policy_revisions WHERE organization_id=$1 AND workspace_id=$2 ORDER BY version DESC LIMIT 1`, scope.OrganizationID().String(), scope.WorkspaceID().String()), &policy); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM ai_egress_usage WHERE organization_id=$1 AND workspace_id=$2 AND phase='authorized' AND outcome='allowed' AND occurred_at>=date_trunc('month',clock_timestamp())`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&request.MonthlyUsed)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return trustcontrol.Policy{}, trustcontrol.ErrDenied
	}
	if err != nil {
		return trustcontrol.Policy{}, err
	}
	return policy, trustcontrol.AuthorizeEgress(policy, request)
}

// AuthorizeAI serializes budget decisions on the current immutable policy and
// records the allow/default-deny decision in the same transaction. This avoids
// a concurrent check-then-insert race at the monthly request boundary.
func (r *Repository) AuthorizeAI(ctx context.Context, scope tenancy.Scope, evidenceID, actor, correlation, accountID string, request trustcontrol.EgressRequest, digest []byte) (trustcontrol.Policy, error) {
	var policy trustcontrol.Policy
	var decisionErr error
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		if err := lockAIPolicy(ctx, tx, scope); err != nil {
			return err
		}
		err := scanPolicy(tx.QueryRowContext(ctx, `SELECT version,enabled,allowed_data_classes,allowed_providers,allowed_models,max_prompt_bytes,monthly_request_limit,created_at FROM ai_egress_policy_revisions WHERE organization_id=$1 AND workspace_id=$2 ORDER BY version DESC LIMIT 1`, scope.OrganizationID().String(), scope.WorkspaceID().String()), &policy)
		if errors.Is(err, sql.ErrNoRows) {
			decisionErr = trustcontrol.ErrDenied
			return insertEvidence(ctx, tx, scope, trustcontrol.Evidence{ID: evidenceID + ".e", Type: "ai.egress.authorized", ActorRef: actor, ResourceType: "ai_provider_account", ResourceID: accountID, CorrelationID: correlation, Decision: "denied", RequestSHA256: digest, Summary: map[string]any{"provider": request.Destination, "model": request.Model, "prompt_bytes": request.PromptBytes, "reason": "no_policy"}, OccurredAt: time.Now().UTC()})
		}
		if err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM ai_egress_usage WHERE organization_id=$1 AND workspace_id=$2 AND phase='authorized' AND outcome='allowed' AND occurred_at>=date_trunc('month',clock_timestamp())`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&request.MonthlyUsed); err != nil {
			return err
		}
		outcome := "allowed"
		if err := trustcontrol.AuthorizeEgress(policy, request); err != nil {
			outcome = "denied"
			decisionErr = trustcontrol.ErrDenied
		}
		return recordAIInTx(ctx, tx, scope, evidenceID, actor, correlation, accountID, "authorized", outcome, request, policy, digest)
	})
	if err != nil {
		return trustcontrol.Policy{}, err
	}
	return policy, decisionErr
}

func lockAIPolicy(ctx context.Context, tx *sql.Tx, scope tenancy.Scope) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "ai-egress:"+scope.OrganizationID().String()+":"+scope.WorkspaceID().String())
	return err
}

// RecordAI records authorization or outcome evidence without prompt bodies.
func (r *Repository) RecordAI(ctx context.Context, scope tenancy.Scope, evidenceID, actor, correlation, accountID, phase, outcome string, request trustcontrol.EgressRequest, policy trustcontrol.Policy, digest []byte) error {
	return r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		return recordAIInTx(ctx, tx, scope, evidenceID, actor, correlation, accountID, phase, outcome, request, policy, digest)
	})
}

func recordAIInTx(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, evidenceID, actor, correlation, accountID, phase, outcome string, request trustcontrol.EgressRequest, policy trustcontrol.Policy, digest []byte) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO ai_egress_usage(id,organization_id,workspace_id,policy_version,account_id,provider,model,phase,outcome,prompt_bytes,prompt_sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, evidenceID, scope.OrganizationID().String(), scope.WorkspaceID().String(), policy.Version, accountID, request.Destination, request.Model, phase, outcome, request.PromptBytes, digest)
	if err != nil {
		return err
	}
	return insertEvidence(ctx, tx, scope, trustcontrol.Evidence{ID: evidenceID + ".e", Type: "ai.egress." + phase, ActorRef: actor, ResourceType: "ai_provider_account", ResourceID: accountID, CorrelationID: correlation, Decision: outcome, RequestSHA256: digest, Summary: map[string]any{"provider": request.Destination, "model": request.Model, "policy_version": policy.Version, "prompt_bytes": request.PromptBytes}, OccurredAt: time.Now().UTC()})
}

// CreateReplay persists one synthetic, no-remote-call replay and its evidence.
func (r *Repository) CreateReplay(ctx context.Context, scope tenancy.Scope, id, actor, key, family, capability string, fixture, result json.RawMessage, digest []byte, status string) (ReplayRun, bool, error) {
	var run ReplayRun
	replayed := false
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		receipt, claimed, err := beginReceipt(ctx, tx, scope, "connector_replay.run", key, digest)
		if err != nil {
			return err
		}
		if !claimed {
			if receipt.State != "completed" {
				return ErrConflict
			}
			replayed = true
			id = receipt.ResourceID
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO connector_replay_runs(id,organization_id,workspace_id,connector_family,capability,fixture_sha256,fixture,result,status,actor_ref) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, scope.OrganizationID().String(), scope.WorkspaceID().String(), family, capability, digest, fixture, result, status, actor)
			if err != nil {
				return err
			}
			if err := insertEvidence(ctx, tx, scope, trustcontrol.Evidence{ID: id + ".e", Type: "connector.replay", ActorRef: actor, ResourceType: "connector_replay", ResourceID: id, CorrelationID: key, Decision: "succeeded", RequestSHA256: digest, Summary: map[string]any{"family": family, "capability": capability, "status": status, "remote_calls": 0}, OccurredAt: time.Now().UTC()}); err != nil {
				return err
			}
			if err := completeReceipt(ctx, tx, scope, "connector_replay.run", key, "connector_replay", id, map[string]any{"id": id}); err != nil {
				return err
			}
		}
		return tx.QueryRowContext(ctx, `SELECT id,connector_family,capability,status,actor_ref,fixture_sha256,fixture,result,created_at FROM connector_replay_runs WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id).Scan(&run.ID, &run.ConnectorFamily, &run.Capability, &run.Status, &run.ActorRef, &run.FixtureSHA256, &run.Fixture, &run.Result, &run.CreatedAt)
	})
	return run, replayed, err
}

// CreateScenario persists one immutable calculation and its evidence.
func (r *Repository) CreateScenario(ctx context.Context, scope tenancy.Scope, id, actor, key string, input trustcontrol.ScenarioInput, result trustcontrol.ScenarioResult, inputJSON, resultJSON, digest []byte) (Scenario, bool, error) {
	var scenario Scenario
	replayed := false
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		receipt, claimed, err := beginReceipt(ctx, tx, scope, "profitability_scenario.create", key, digest)
		if err != nil {
			return err
		}
		if !claimed {
			if receipt.State != "completed" {
				return ErrConflict
			}
			replayed = true
			id = receipt.ResourceID
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO profitability_scenarios(id,organization_id,workspace_id,name,algorithm_version,input_snapshot,result_snapshot,input_sha256,actor_ref) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, scope.OrganizationID().String(), scope.WorkspaceID().String(), strings.TrimSpace(input.Name), result.AlgorithmVersion, inputJSON, resultJSON, digest, actor)
			if err != nil {
				return err
			}
			if err := insertEvidence(ctx, tx, scope, trustcontrol.Evidence{ID: id + ".e", Type: "profitability.scenario", ActorRef: actor, ResourceType: "profitability_scenario", ResourceID: id, CorrelationID: key, Decision: "succeeded", RequestSHA256: digest, Summary: map[string]any{"algorithm_version": result.AlgorithmVersion, "currency": result.Currency}, OccurredAt: time.Now().UTC()}); err != nil {
				return err
			}
			if err := completeReceipt(ctx, tx, scope, "profitability_scenario.create", key, "profitability_scenario", id, map[string]any{"id": id}); err != nil {
				return err
			}
		}
		return tx.QueryRowContext(ctx, `SELECT id,name,algorithm_version,actor_ref,input_snapshot,result_snapshot,input_sha256,created_at FROM profitability_scenarios WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id).Scan(&scenario.ID, &scenario.Name, &scenario.AlgorithmVersion, &scenario.ActorRef, &scenario.Input, &scenario.Result, &scenario.InputSHA256, &scenario.CreatedAt)
	})
	return scenario, replayed, err
}

func scanPolicy(row interface{ Scan(...any) error }, policy *trustcontrol.Policy) error {
	var classes, destinations, models []byte
	if err := row.Scan(&policy.Version, &policy.Enabled, &classes, &destinations, &models, &policy.MaxPromptBytes, &policy.MonthlyRequestLimit, &policy.CreatedAt); err != nil {
		return err
	}
	if json.Unmarshal(classes, &policy.AllowedDataClasses) != nil || json.Unmarshal(destinations, &policy.AllowedDestinations) != nil || json.Unmarshal(models, &policy.AllowedModels) != nil {
		return ErrInvalid
	}
	return nil
}

func nullableDigest(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
