// Package marketplaceoperationsrepo persists the tenant-scoped marketplace
// orchestration projection and its idempotent command journal.
package marketplaceoperationsrepo

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/marketplaceoperations"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const applyScopeStatement = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

const flowColumns = `flow_id,organization_id,workspace_id,account_id,stage,state,version,last_operation_id,last_idempotency_key,last_reason_code,last_command_digest,references_json,created_at,updated_at`

// Repository stores only the normalized flow and command metadata. Provider
// payloads, access tokens and secret material have no column in this package.
type Repository struct{ db *sql.DB }

var _ marketplaceoperations.FlowRepository = (*Repository)(nil)
var _ marketplaceoperations.FindingRepository = (*Repository)(nil)

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("marketplace operations repository: database required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Create(ctx context.Context, scope tenancy.Scope, flow marketplaceoperations.Flow) error {
	if err := r.validate(ctx, scope); err != nil {
		return err
	}
	if flow.Validate() != nil || flow.OrganizationID != scope.OrganizationID().String() || flow.WorkspaceID != scope.WorkspaceID().String() {
		return marketplaceoperations.ErrInvalidFlow
	}
	referencesValue := flow.References
	if referencesValue == nil {
		referencesValue = []marketplaceoperations.Reference{}
	}
	references, err := json.Marshal(referencesValue)
	if err != nil {
		return marketplaceoperations.ErrInvalidFlow
	}
	err = r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var inserted string
		err := tx.QueryRowContext(ctx, `INSERT INTO marketplace_operation_flows (flow_id,organization_id,workspace_id,account_id,stage,state,version,last_operation_id,last_idempotency_key,last_reason_code,last_command_digest,references_json,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT DO NOTHING RETURNING flow_id`, flow.ID, flow.OrganizationID, flow.WorkspaceID, flow.AccountID, string(flow.Stage), string(flow.State), flow.Version, flow.LastOperationID, flow.LastIdempotencyKey, flow.LastReasonCode, flow.LastCommandDigest, references, flow.CreatedAt, flow.UpdatedAt).Scan(&inserted)
		if errors.Is(err, sql.ErrNoRows) {
			return marketplaceoperations.ErrFlowConflict
		}
		return err
	})
	return err
}

func (r *Repository) Flow(ctx context.Context, scope tenancy.Scope, id string) (marketplaceoperations.Flow, error) {
	if err := r.validate(ctx, scope); err != nil {
		return marketplaceoperations.Flow{}, err
	}
	if strings.TrimSpace(id) == "" || len(id) > 192 {
		return marketplaceoperations.Flow{}, marketplaceoperations.ErrInvalidFlow
	}
	var out marketplaceoperations.Flow
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		out, err = scanFlow(tx.QueryRowContext(ctx, `SELECT `+flowColumns+` FROM marketplace_operation_flows WHERE organization_id=$1 AND workspace_id=$2 AND flow_id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id))
		return err
	})
	return out, err
}

func (r *Repository) List(ctx context.Context, scope tenancy.Scope, cursor string, limit int) (marketplaceoperations.FlowPage, error) {
	if err := r.validate(ctx, scope); err != nil {
		return marketplaceoperations.FlowPage{}, err
	}
	if limit < 1 || limit > 100 {
		return marketplaceoperations.FlowPage{}, marketplaceoperations.ErrInvalidFlow
	}
	position, err := decodeCursor(cursor)
	if err != nil {
		return marketplaceoperations.FlowPage{}, marketplaceoperations.ErrInvalidFlow
	}
	items := make([]marketplaceoperations.Flow, 0, limit)
	err = r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var rows *sql.Rows
		if position.ID == "" {
			rows, err = tx.QueryContext(ctx, `SELECT `+flowColumns+` FROM marketplace_operation_flows WHERE organization_id=$1 AND workspace_id=$2 ORDER BY updated_at DESC,flow_id DESC LIMIT $3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), limit+1)
		} else {
			rows, err = tx.QueryContext(ctx, `SELECT `+flowColumns+` FROM marketplace_operation_flows WHERE organization_id=$1 AND workspace_id=$2 AND (updated_at,flow_id)<($3,$4) ORDER BY updated_at DESC,flow_id DESC LIMIT $5`, scope.OrganizationID().String(), scope.WorkspaceID().String(), position.UpdatedAt, position.ID, limit+1)
		}
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			flow, scanErr := scanFlow(rows)
			if scanErr != nil {
				return scanErr
			}
			items = append(items, flow)
		}
		return rows.Err()
	})
	if err != nil {
		return marketplaceoperations.FlowPage{}, err
	}
	page := marketplaceoperations.FlowPage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		page.Items = items[:limit]
		page.NextCursor, err = encodeCursor(flowCursor{UpdatedAt: last.UpdatedAt, ID: last.ID})
		if err != nil {
			return marketplaceoperations.FlowPage{}, marketplaceoperations.ErrInvalidFlow
		}
	}
	return page, nil
}

func (r *Repository) Apply(ctx context.Context, scope tenancy.Scope, flowID string, command marketplaceoperations.Command) (marketplaceoperations.Flow, bool, error) {
	if err := r.validate(ctx, scope); err != nil {
		return marketplaceoperations.Flow{}, false, err
	}
	if strings.TrimSpace(flowID) == "" || command.Validate() != nil {
		return marketplaceoperations.Flow{}, false, marketplaceoperations.ErrInvalidFlow
	}
	var out marketplaceoperations.Flow
	duplicate := false
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var err error
		out, err = scanFlow(tx.QueryRowContext(ctx, `SELECT `+flowColumns+` FROM marketplace_operation_flows WHERE organization_id=$1 AND workspace_id=$2 AND flow_id=$3 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), flowID))
		if errors.Is(err, marketplaceoperations.ErrFlowNotFound) {
			return err
		}
		if err != nil {
			return err
		}
		var existingOperation, existingStage, existingOutcome, existingReason string
		var existingReferences []byte
		var existingOccurredAt time.Time
		err = tx.QueryRowContext(ctx, `SELECT operation_id,stage,outcome,reason_code,references_json,occurred_at FROM marketplace_operation_commands WHERE organization_id=$1 AND workspace_id=$2 AND flow_id=$3 AND idempotency_key=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), flowID, command.IdempotencyKey).Scan(&existingOperation, &existingStage, &existingOutcome, &existingReason, &existingReferences, &existingOccurredAt)
		if err == nil {
			if existingOperation != command.OperationID || existingStage != string(command.Stage) || existingOutcome != string(command.Outcome) || existingReason != command.ReasonCode || !existingOccurredAt.UTC().Equal(command.OccurredAt.UTC()) || !sameJSONArray(existingReferences, command.References) {
				return marketplaceoperations.ErrDuplicateConflict
			}
			duplicate = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		updated, _, err := marketplaceoperations.Apply(out, command)
		if err != nil {
			return err
		}
		commandReferences := command.References
		if commandReferences == nil {
			commandReferences = []marketplaceoperations.Reference{}
		}
		references, marshalErr := json.Marshal(commandReferences)
		if marshalErr != nil {
			return marketplaceoperations.ErrInvalidFlow
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO marketplace_operation_commands (organization_id,workspace_id,flow_id,operation_id,idempotency_key,stage,outcome,reason_code,references_json,occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), flowID, command.OperationID, command.IdempotencyKey, string(command.Stage), string(command.Outcome), command.ReasonCode, references, command.OccurredAt); err != nil {
			return err
		}
		updatedReferencesValue := updated.References
		if updatedReferencesValue == nil {
			updatedReferencesValue = []marketplaceoperations.Reference{}
		}
		updatedReferences, marshalErr := json.Marshal(updatedReferencesValue)
		if marshalErr != nil {
			return marketplaceoperations.ErrInvalidFlow
		}
		result, err := tx.ExecContext(ctx, `UPDATE marketplace_operation_flows SET stage=$4,state=$5,version=$6,last_operation_id=$7,last_idempotency_key=$8,last_reason_code=$9,last_command_digest=$10,references_json=$11,updated_at=$12 WHERE organization_id=$1 AND workspace_id=$2 AND flow_id=$3 AND version=$13`, scope.OrganizationID().String(), scope.WorkspaceID().String(), flowID, string(updated.Stage), string(updated.State), updated.Version, updated.LastOperationID, updated.LastIdempotencyKey, updated.LastReasonCode, updated.LastCommandDigest, updatedReferences, updated.UpdatedAt, out.Version)
		if err != nil {
			return err
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
			return marketplaceoperations.ErrFlowConflict
		}
		out = updated
		return nil
	})
	return out, duplicate, mapError(err)
}

const findingColumns = `f.finding_id,f.organization_id,f.workspace_id,f.flow_id,f.account_id,f.stage,f.kind,f.entity_kind,f.entity_id,f.severity,CASE WHEN EXISTS (SELECT 1 FROM marketplace_operation_finding_actions a WHERE a.organization_id=f.organization_id AND a.workspace_id=f.workspace_id AND a.finding_id=f.finding_id AND a.action='resolve') THEN 'resolved' ELSE f.status END,f.reason_code,f.expected_value,f.observed_value,f.evidence_digest,f.detected_at`

// RecordFinding appends one immutable reconciliation finding. A repeated
// finding identity is treated as a conflict so callers cannot silently replace
// the original evidence.
func (r *Repository) RecordFinding(ctx context.Context, scope tenancy.Scope, finding marketplaceoperations.Finding) error {
	if err := r.validate(ctx, scope); err != nil {
		return err
	}
	if finding.Validate() != nil || finding.OrganizationID != scope.OrganizationID().String() || finding.WorkspaceID != scope.WorkspaceID().String() {
		return marketplaceoperations.ErrInvalidFinding
	}
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var inserted string
		err := tx.QueryRowContext(ctx, `INSERT INTO marketplace_operation_findings (organization_id,workspace_id,finding_id,flow_id,account_id,stage,kind,entity_kind,entity_id,severity,status,reason_code,expected_value,observed_value,evidence_digest,detected_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'open',$11,$12,$13,$14,$15) ON CONFLICT DO NOTHING RETURNING finding_id`, finding.OrganizationID, finding.WorkspaceID, finding.ID, finding.FlowID, finding.AccountID, string(finding.Stage), string(finding.Kind), finding.EntityKind, finding.EntityID, string(finding.Severity), finding.ReasonCode, finding.Expected, finding.Observed, finding.EvidenceDigest, finding.DetectedAt).Scan(&inserted)
		if errors.Is(err, sql.ErrNoRows) {
			return marketplaceoperations.ErrFindingConflict
		}
		return err
	})
	return err
}

// Finding returns one tenant-scoped finding with status derived from its
// append-only action journal.
func (r *Repository) Finding(ctx context.Context, scope tenancy.Scope, id string) (marketplaceoperations.Finding, error) {
	if err := r.validate(ctx, scope); err != nil {
		return marketplaceoperations.Finding{}, err
	}
	if strings.TrimSpace(id) == "" || len(id) > 192 {
		return marketplaceoperations.Finding{}, marketplaceoperations.ErrInvalidFinding
	}
	var out marketplaceoperations.Finding
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		return scanFinding(tx.QueryRowContext(ctx, `SELECT `+findingColumns+` FROM marketplace_operation_findings f WHERE f.organization_id=$1 AND f.workspace_id=$2 AND f.finding_id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id), &out)
	})
	return out, mapFindingError(err)
}

// ListFindings returns a bounded, cursor-paginated operations-center view.
func (r *Repository) ListFindings(ctx context.Context, scope tenancy.Scope, query marketplaceoperations.FindingQuery) (marketplaceoperations.FindingPage, error) {
	if err := r.validate(ctx, scope); err != nil {
		return marketplaceoperations.FindingPage{}, err
	}
	if query.Validate() != nil {
		return marketplaceoperations.FindingPage{}, marketplaceoperations.ErrInvalidFinding
	}
	position, err := decodeFindingCursor(query.Cursor)
	if err != nil {
		return marketplaceoperations.FindingPage{}, err
	}
	items := make([]marketplaceoperations.Finding, 0, query.Limit)
	err = r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		args := []any{scope.OrganizationID().String(), scope.WorkspaceID().String()}
		where := `f.organization_id=$1 AND f.workspace_id=$2`
		nextArg := 3
		if query.FlowID != "" {
			where += fmt.Sprintf(" AND f.flow_id=$%d", nextArg)
			args = append(args, query.FlowID)
			nextArg++
		}
		if query.Status == marketplaceoperations.FindingOpen {
			where += ` AND NOT EXISTS (SELECT 1 FROM marketplace_operation_finding_actions ao WHERE ao.organization_id=f.organization_id AND ao.workspace_id=f.workspace_id AND ao.finding_id=f.finding_id AND ao.action='resolve')`
		} else if query.Status == marketplaceoperations.FindingResolved {
			where += ` AND EXISTS (SELECT 1 FROM marketplace_operation_finding_actions ar WHERE ar.organization_id=f.organization_id AND ar.workspace_id=f.workspace_id AND ar.finding_id=f.finding_id AND ar.action='resolve')`
		}
		if position.ID != "" {
			where += fmt.Sprintf(" AND (f.detected_at,f.finding_id)<($%d,$%d)", nextArg, nextArg+1)
			args = append(args, position.DetectedAt, position.ID)
			nextArg += 2
		}
		args = append(args, query.Limit+1)
		rows, queryErr := tx.QueryContext(ctx, `SELECT `+findingColumns+` FROM marketplace_operation_findings f WHERE `+where+fmt.Sprintf(` ORDER BY f.detected_at DESC,f.finding_id DESC LIMIT $%d`, nextArg), args...)
		if queryErr != nil {
			return queryErr
		}
		defer rows.Close()
		for rows.Next() {
			var item marketplaceoperations.Finding
			if scanErr := scanFinding(rows, &item); scanErr != nil {
				return scanErr
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return marketplaceoperations.FindingPage{}, err
	}
	page := marketplaceoperations.FindingPage{Items: items}
	if len(items) > query.Limit {
		last := items[query.Limit-1]
		page.Items = items[:query.Limit]
		page.NextCursor, err = encodeFindingCursor(findingCursor{DetectedAt: last.DetectedAt, ID: last.ID})
		if err != nil {
			return marketplaceoperations.FindingPage{}, marketplaceoperations.ErrInvalidFinding
		}
	}
	return page, nil
}

// ApplyFindingAction appends a retry/reconcile/resolve decision. It never
// performs a connector call; a bounded worker consumes the durable intent.
func (r *Repository) ApplyFindingAction(ctx context.Context, scope tenancy.Scope, findingID string, action marketplaceoperations.FindingAction) (marketplaceoperations.FindingAction, bool, error) {
	if err := r.validate(ctx, scope); err != nil {
		return marketplaceoperations.FindingAction{}, false, err
	}
	if action.Validate() != nil || action.FindingID != findingID {
		return marketplaceoperations.FindingAction{}, false, marketplaceoperations.ErrInvalidFinding
	}
	var out marketplaceoperations.FindingAction
	duplicate := false
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var existing marketplaceoperations.FindingAction
		row := tx.QueryRowContext(ctx, `SELECT action_id,finding_id,action,idempotency_key,actor_id,occurred_at FROM marketplace_operation_finding_actions WHERE organization_id=$1 AND workspace_id=$2 AND finding_id=$3 AND idempotency_key=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), findingID, action.IdempotencyKey)
		if scanErr := scanFindingAction(row, &existing); scanErr == nil {
			if existing.Action != action.Action || existing.ActorID != action.ActorID {
				return marketplaceoperations.ErrDuplicateConflict
			}
			out, duplicate = existing, true
			return nil
		} else if !errors.Is(scanErr, sql.ErrNoRows) {
			return scanErr
		}
		if err := tx.QueryRowContext(ctx, `SELECT finding_id FROM marketplace_operation_findings WHERE organization_id=$1 AND workspace_id=$2 AND finding_id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), findingID).Scan(new(string)); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return marketplaceoperations.ErrFlowNotFound
			}
			return err
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO marketplace_operation_finding_actions (organization_id,workspace_id,finding_id,action_id,action,idempotency_key,actor_id,occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING action_id,finding_id,action,idempotency_key,actor_id,occurred_at`, scope.OrganizationID().String(), scope.WorkspaceID().String(), findingID, action.ID, string(action.Action), action.IdempotencyKey, action.ActorID, action.OccurredAt).Scan(&out.ID, &out.FindingID, &out.Action, &out.IdempotencyKey, &out.ActorID, &out.OccurredAt); err != nil {
			return err
		}
		out.OccurredAt = out.OccurredAt.UTC()
		return nil
	})
	return out, duplicate, mapFindingError(err)
}

type findingCursor struct {
	DetectedAt time.Time `json:"detected_at"`
	ID         string    `json:"id"`
}

func encodeFindingCursor(value findingCursor) (string, error) {
	if value.ID == "" || value.DetectedAt.IsZero() || value.DetectedAt.Location() != time.UTC {
		return "", marketplaceoperations.ErrInvalidFinding
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeFindingCursor(value string) (findingCursor, error) {
	if value == "" {
		return findingCursor{}, nil
	}
	if len(value) > 512 {
		return findingCursor{}, marketplaceoperations.ErrInvalidFinding
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return findingCursor{}, marketplaceoperations.ErrInvalidFinding
	}
	var out findingCursor
	if json.Unmarshal(data, &out) != nil || out.ID == "" || out.DetectedAt.IsZero() {
		return findingCursor{}, marketplaceoperations.ErrInvalidFinding
	}
	out.DetectedAt = out.DetectedAt.UTC()
	return out, nil
}

func scanFinding(row scanner, out *marketplaceoperations.Finding) error {
	var stage, kind, severity, status string
	if err := row.Scan(&out.ID, &out.OrganizationID, &out.WorkspaceID, &out.FlowID, &out.AccountID, &stage, &kind, &out.EntityKind, &out.EntityID, &severity, &status, &out.ReasonCode, &out.Expected, &out.Observed, &out.EvidenceDigest, &out.DetectedAt); err != nil {
		return err
	}
	out.Stage = marketplaceoperations.FlowStage(stage)
	out.Kind = marketplaceoperations.FindingKind(kind)
	out.Severity = marketplaceoperations.FindingSeverity(severity)
	out.Status = marketplaceoperations.FindingStatus(status)
	out.DetectedAt = out.DetectedAt.UTC()
	if out.Status != marketplaceoperations.FindingOpen && out.Status != marketplaceoperations.FindingResolved {
		return marketplaceoperations.ErrInvalidFinding
	}
	return nil
}

func scanFindingAction(row scanner, out *marketplaceoperations.FindingAction) error {
	var action string
	if err := row.Scan(&out.ID, &out.FindingID, &action, &out.IdempotencyKey, &out.ActorID, &out.OccurredAt); err != nil {
		return err
	}
	out.Action = marketplaceoperations.FindingActionKind(action)
	out.OccurredAt = out.OccurredAt.UTC()
	return out.Validate()
}

func mapFindingError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return marketplaceoperations.ErrFlowNotFound
	}
	return err
}

type flowCursor struct {
	UpdatedAt time.Time `json:"updated_at"`
	ID        string    `json:"id"`
}

func encodeCursor(value flowCursor) (string, error) {
	if value.ID == "" || value.UpdatedAt.IsZero() || value.UpdatedAt.Location() != time.UTC {
		return "", marketplaceoperations.ErrInvalidFlow
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeCursor(value string) (flowCursor, error) {
	if value == "" {
		return flowCursor{}, nil
	}
	if len(value) > 512 {
		return flowCursor{}, marketplaceoperations.ErrInvalidFlow
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return flowCursor{}, marketplaceoperations.ErrInvalidFlow
	}
	var out flowCursor
	if json.Unmarshal(data, &out) != nil || out.ID == "" || out.UpdatedAt.IsZero() || out.UpdatedAt.Location() != time.UTC {
		return flowCursor{}, marketplaceoperations.ErrInvalidFlow
	}
	return out, nil
}

type scanner interface{ Scan(...any) error }

func scanFlow(row scanner) (marketplaceoperations.Flow, error) {
	var out marketplaceoperations.Flow
	var stage, state string
	var lastCommandDigest string
	var refs []byte
	if err := row.Scan(&out.ID, &out.OrganizationID, &out.WorkspaceID, &out.AccountID, &stage, &state, &out.Version, &out.LastOperationID, &out.LastIdempotencyKey, &out.LastReasonCode, &lastCommandDigest, &refs, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return marketplaceoperations.Flow{}, marketplaceoperations.ErrFlowNotFound
		}
		return marketplaceoperations.Flow{}, fmt.Errorf("marketplace operations repository: scan flow: %w", err)
	}
	if json.Unmarshal(refs, &out.References) != nil {
		return marketplaceoperations.Flow{}, marketplaceoperations.ErrInvalidFlow
	}
	out.Stage = marketplaceoperations.FlowStage(stage)
	out.State = marketplaceoperations.FlowState(state)
	out.LastCommandDigest = lastCommandDigest
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	if out.Validate() != nil {
		return marketplaceoperations.Flow{}, marketplaceoperations.ErrInvalidFlow
	}
	return out, nil
}

func (r *Repository) validate(ctx context.Context, scope tenancy.Scope) error {
	if ctx == nil {
		return errors.New("marketplace operations repository: context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.db == nil {
		return errors.New("marketplace operations repository: uninitialized")
	}
	if !scope.Valid() {
		return tenancy.ErrInvalidScope
	}
	return nil
}

func (r *Repository) tx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("marketplace operations repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var organization, workspace string
	if err := tx.QueryRowContext(ctx, applyScopeStatement, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&organization, &workspace); err != nil {
		return fmt.Errorf("marketplace operations repository: scope: %w", err)
	}
	if organization != scope.OrganizationID().String() || workspace != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("marketplace operations repository: commit: %w", err)
	}
	return nil
}

func mapError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return marketplaceoperations.ErrFlowNotFound
	}
	return err
}

func sameJSONArray(existing []byte, references []marketplaceoperations.Reference) bool {
	if references == nil {
		references = []marketplaceoperations.Reference{}
	}
	current, err := json.Marshal(references)
	if err != nil {
		return false
	}
	var existingValue, currentValue any
	if json.Unmarshal(existing, &existingValue) != nil || json.Unmarshal(current, &currentValue) != nil {
		return false
	}
	existingCanonical, existingErr := json.Marshal(existingValue)
	currentCanonical, currentErr := json.Marshal(currentValue)
	return existingErr == nil && currentErr == nil && string(existingCanonical) == string(currentCanonical)
}
