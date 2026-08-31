// Package operatorassistantrepo persists bounded tenant-scoped assistant
// sessions, runs, normalized answers and typed preview metadata. Raw prompts,
// provider payloads and credentials are never written.
package operatorassistantrepo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/operatorassistant"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var (
	ErrInvalid  = errors.New("operator assistant repository: invalid")
	ErrNotFound = errors.New("operator assistant repository: not found")
	ErrConflict = errors.New("operator assistant repository: conflict")
)

var assistantErrorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,95}$`)

type Repository struct{ db *sql.DB }

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &Repository{db: db}, nil
}

func (r *Repository) withTx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || fn == nil {
		return ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return fmt.Errorf("assistant scope: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

const sessionColumns = `id,organization_id,workspace_id,actor_id,title,locale,version,created_at,updated_at`

func scanSession(row interface{ Scan(...any) error }) (operatorassistant.Session, error) {
	var out operatorassistant.Session
	if err := row.Scan(&out.ID, &out.OrganizationID, &out.WorkspaceID, &out.ActorID, &out.Title, &out.Locale, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return operatorassistant.Session{}, ErrNotFound
		}
		return operatorassistant.Session{}, err
	}
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	if out.Validate() != nil {
		return operatorassistant.Session{}, ErrInvalid
	}
	return out, nil
}

func (r *Repository) CreateSession(ctx context.Context, scope tenancy.Scope, session operatorassistant.Session) (operatorassistant.Session, error) {
	if session.Validate() != nil || session.OrganizationID != scope.OrganizationID().String() || session.WorkspaceID != scope.WorkspaceID().String() {
		return operatorassistant.Session{}, ErrInvalid
	}
	var out operatorassistant.Session
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `INSERT INTO assistant_sessions(id,organization_id,workspace_id,actor_id,title,locale,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+sessionColumns,
			session.ID, session.OrganizationID, session.WorkspaceID, session.ActorID, session.Title, session.Locale, session.Version, session.CreatedAt, session.UpdatedAt)
		var err error
		out, err = scanSession(row)
		return err
	})
	return out, err
}

func (r *Repository) ListSessions(ctx context.Context, scope tenancy.Scope, actorID string, limit int) ([]operatorassistant.Session, error) {
	if strings.TrimSpace(actorID) == "" || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	out := make([]operatorassistant.Session, 0)
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+sessionColumns+` FROM assistant_sessions WHERE organization_id=$1 AND workspace_id=$2 AND actor_id=$3 ORDER BY updated_at DESC,id DESC LIMIT $4`,
			scope.OrganizationID().String(), scope.WorkspaceID().String(), actorID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			session, err := scanSession(rows)
			if err != nil {
				return err
			}
			out = append(out, session)
		}
		return rows.Err()
	})
	return out, err
}

func (r *Repository) GetSession(ctx context.Context, scope tenancy.Scope, actorID, id string) (operatorassistant.Session, error) {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(id) == "" {
		return operatorassistant.Session{}, ErrInvalid
	}
	var out operatorassistant.Session
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		out, err = scanSession(tx.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM assistant_sessions WHERE organization_id=$1 AND workspace_id=$2 AND actor_id=$3 AND id=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), actorID, id))
		return err
	})
	return out, err
}

const runColumns = `id,session_id,organization_id,workspace_id,actor_id,state,intent,context_digest,answer,answer_digest,error_code,version,created_at,updated_at`

func scanRun(row interface{ Scan(...any) error }) (operatorassistant.Run, error) {
	var out operatorassistant.Run
	var state, intent string
	var rawAnswer []byte
	var answerDigest sql.NullString
	if err := row.Scan(&out.ID, &out.SessionID, &out.OrganizationID, &out.WorkspaceID, &out.ActorID, &state, &intent, &out.ContextDigest, &rawAnswer, &answerDigest, &out.ErrorCode, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return operatorassistant.Run{}, ErrNotFound
		}
		return operatorassistant.Run{}, err
	}
	out.State = operatorassistant.RunState(state)
	out.Intent = operatorassistant.Intent(intent)
	if len(rawAnswer) > 0 && string(rawAnswer) != "null" {
		var answer operatorassistant.Answer
		if err := json.Unmarshal(rawAnswer, &answer); err != nil {
			return operatorassistant.Run{}, ErrInvalid
		}
		out.Answer = &answer
	}
	if answerDigest.Valid && out.Answer != nil && out.Answer.AnswerDigest == "" {
		out.Answer.AnswerDigest = answerDigest.String
	}
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	if out.Validate(time.Now().UTC()) != nil {
		return operatorassistant.Run{}, ErrInvalid
	}
	return out, nil
}

func (r *Repository) CreateRun(ctx context.Context, scope tenancy.Scope, run operatorassistant.Run) (operatorassistant.Run, error) {
	if run.Validate(time.Now().UTC()) != nil || run.OrganizationID != scope.OrganizationID().String() || run.WorkspaceID != scope.WorkspaceID().String() {
		return operatorassistant.Run{}, ErrInvalid
	}
	var out operatorassistant.Run
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		raw := []byte("null")
		if run.Answer != nil {
			var err error
			raw, err = json.Marshal(run.Answer)
			if err != nil {
				return ErrInvalid
			}
		}
		var err error
		out, err = scanRun(tx.QueryRowContext(ctx, `INSERT INTO assistant_runs(id,session_id,organization_id,workspace_id,actor_id,state,intent,context_digest,answer,answer_digest,error_code,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,NULLIF($10,''),$11,$12,$13,$14) RETURNING `+runColumns,
			run.ID, run.SessionID, run.OrganizationID, run.WorkspaceID, run.ActorID, string(run.State), string(run.Intent), run.ContextDigest, raw, answerDigest(run), run.ErrorCode, run.Version, run.CreatedAt, run.UpdatedAt))
		return err
	})
	return out, err
}

func answerDigest(run operatorassistant.Run) string {
	if run.Answer == nil {
		return ""
	}
	return run.Answer.AnswerDigest
}

func (r *Repository) GetRun(ctx context.Context, scope tenancy.Scope, actorID, id string) (operatorassistant.Run, error) {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(id) == "" {
		return operatorassistant.Run{}, ErrInvalid
	}
	var out operatorassistant.Run
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		out, err = scanRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM assistant_runs WHERE organization_id=$1 AND workspace_id=$2 AND actor_id=$3 AND id=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), actorID, id))
		return err
	})
	return out, err
}

// GetRunForWorker is the worker-only read boundary. The dispatch lease has
// already supplied tenant scope, so no actor selector is accepted here.
func (r *Repository) GetRunForWorker(ctx context.Context, scope tenancy.Scope, id string) (operatorassistant.Run, error) {
	if strings.TrimSpace(id) == "" {
		return operatorassistant.Run{}, ErrInvalid
	}
	var out operatorassistant.Run
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		out, err = scanRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM assistant_runs WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id))
		return err
	})
	return out, err
}

func (r *Repository) SaveAnswer(ctx context.Context, scope tenancy.Scope, actorID, runID string, expectedVersion int64, state operatorassistant.RunState, answer operatorassistant.Answer, now time.Time) (operatorassistant.Run, error) {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(runID) == "" || expectedVersion < 1 || !state.Valid() || answer.Validate(now) != nil || !utc(now) {
		return operatorassistant.Run{}, ErrInvalid
	}
	raw, err := json.Marshal(answer)
	if err != nil {
		return operatorassistant.Run{}, ErrInvalid
	}
	var out operatorassistant.Run
	err = r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `UPDATE assistant_runs SET state=$6,answer=$7::jsonb,answer_digest=$8,version=version+1,updated_at=$9 WHERE organization_id=$1 AND workspace_id=$2 AND actor_id=$3 AND id=$4 AND version=$5 AND state NOT IN ('cancelled','failed') RETURNING `+runColumns,
			scope.OrganizationID().String(), scope.WorkspaceID().String(), actorID, runID, expectedVersion, string(state), raw, answer.AnswerDigest, now)
		var scanErr error
		out, scanErr = scanRun(row)
		if errors.Is(scanErr, ErrNotFound) {
			return ErrConflict
		}
		return scanErr
	})
	return out, err
}

// TransitionRun advances a queued run without fabricating an answer. It is
// used by the durable worker when a provider is not configured or a lease is
// recovered after a crash; a terminal provider_unavailable state remains
// visible to the operator and can be retried explicitly.
func (r *Repository) TransitionRun(ctx context.Context, scope tenancy.Scope, runID string, expectedVersion int64, state operatorassistant.RunState, errorCode string, now time.Time) (operatorassistant.Run, error) {
	if strings.TrimSpace(runID) == "" || expectedVersion < 1 || !state.Valid() || state == operatorassistant.RunCompleted || state == operatorassistant.RunPartial || state == operatorassistant.RunStale || state == operatorassistant.RunBlocked || (errorCode != "" && !assistantErrorCodePattern.MatchString(errorCode)) || !utc(now) {
		return operatorassistant.Run{}, ErrInvalid
	}
	var out operatorassistant.Run
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `UPDATE assistant_runs SET state=$4,error_code=$5,version=version+1,updated_at=$6 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$7 AND state NOT IN ('completed','partial','stale','blocked','cancelled','failed') RETURNING `+runColumns, scope.OrganizationID().String(), scope.WorkspaceID().String(), runID, string(state), errorCode, now, expectedVersion)
		var scanErr error
		out, scanErr = scanRun(row)
		if errors.Is(scanErr, ErrNotFound) {
			return ErrConflict
		}
		return scanErr
	})
	return out, err
}

func (r *Repository) CancelRun(ctx context.Context, scope tenancy.Scope, actorID, runID string, expectedVersion int64, now time.Time) (operatorassistant.Run, error) {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(runID) == "" || expectedVersion < 1 || !utc(now) {
		return operatorassistant.Run{}, ErrInvalid
	}
	var out operatorassistant.Run
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `UPDATE assistant_runs SET state='cancelled',version=version+1,updated_at=$5 WHERE organization_id=$1 AND workspace_id=$2 AND actor_id=$3 AND id=$4 AND version=$6 AND state IN ('queued','retrieving_context','awaiting_model','streaming') RETURNING `+runColumns,
			scope.OrganizationID().String(), scope.WorkspaceID().String(), actorID, runID, now, expectedVersion)
		var scanErr error
		out, scanErr = scanRun(row)
		if errors.Is(scanErr, ErrNotFound) {
			return ErrConflict
		}
		return scanErr
	})
	return out, err
}

func (r *Repository) RecordFeedback(ctx context.Context, scope tenancy.Scope, actorID string, feedback operatorassistant.Feedback, now time.Time) error {
	if strings.TrimSpace(actorID) == "" || feedback.Validate() != nil || !utc(now) {
		return ErrInvalid
	}
	return r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO assistant_feedback(organization_id,workspace_id,actor_id,run_id,kind,reason_code,created_at)
SELECT $1,$2,$3,id,$5,$6,$7 FROM assistant_runs
WHERE organization_id=$1 AND workspace_id=$2 AND actor_id=$3 AND id=$4
ON CONFLICT (organization_id,workspace_id,actor_id,run_id) DO UPDATE SET kind=EXCLUDED.kind,reason_code=EXCLUDED.reason_code,created_at=EXCLUDED.created_at`,
			scope.OrganizationID().String(), scope.WorkspaceID().String(), actorID, feedback.RunID, string(feedback.Kind), feedback.ReasonCode, now)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// CreateActionPreview persists the non-executing preview next to its run. The
// table intentionally stores only normalized metadata and evidence digests;
// impact text and capability details are recomputed/omitted at the API edge.
func (r *Repository) CreateActionPreview(ctx context.Context, scope tenancy.Scope, runID, actorID string, preview operatorassistant.ActionPreview, now time.Time) error {
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(actorID) == "" || preview.Validate(now) != nil || !utc(now) {
		return ErrInvalid
	}
	return r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		var existingDigest string
		err := tx.QueryRowContext(ctx, `SELECT preview_digest FROM assistant_action_previews WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), preview.ID).Scan(&existingDigest)
		if err == nil {
			if existingDigest != preview.PreviewDigest {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO assistant_action_previews(organization_id,workspace_id,id,run_id,actor_id,action,resource_type,resource_id,expected_version,risk,required_permission,preview_digest,evidence_digest,status,expires_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'pending',$14,$15)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), preview.ID, runID, actorID, preview.Action, preview.ResourceType, preview.ResourceID, preview.ExpectedVersion, string(preview.Risk), preview.RequiredPermission, preview.PreviewDigest, preview.EvidenceDigest, preview.ExpiresAt, now)
		return err
	})
}

// GetActionPreview loads a tenant/actor-scoped preview. It returns terminal
// previews as well so the UI can explain an idempotent approval/rejection.
func (r *Repository) GetActionPreview(ctx context.Context, scope tenancy.Scope, actorID, id string) (operatorassistant.ActionPreview, error) {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(id) == "" {
		return operatorassistant.ActionPreview{}, ErrInvalid
	}
	var out operatorassistant.ActionPreview
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var risk, status string
		var expires, created time.Time
		err := tx.QueryRowContext(ctx, `SELECT id,action,resource_type,resource_id,expected_version,risk,required_permission,preview_digest,evidence_digest,status,expires_at,created_at FROM assistant_action_previews WHERE organization_id=$1 AND workspace_id=$2 AND actor_id=$3 AND id=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), actorID, id).Scan(&out.ID, &out.Action, &out.ResourceType, &out.ResourceID, &out.ExpectedVersion, &risk, &out.RequiredPermission, &out.PreviewDigest, &out.EvidenceDigest, &status, &expires, &created)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		out.Risk, out.Status, out.ExpiresAt = operatorassistant.Risk(risk), status, expires.UTC()
		out.IdempotencyKey = "assistant-preview-" + digestID(out.Action+"\x00"+out.ResourceType+"\x00"+out.ResourceID+"\x00"+out.EvidenceDigest)
		out.Impact = "Проверка и выполнение остаются за владельцем домена."
		out.ApprovalRequired = out.Risk == operatorassistant.RiskSensitiveWrite
		if out.ExpiresAt.Before(created.UTC()) {
			return ErrInvalid
		}
		return nil
	})
	return out, err
}

// MarkActionPreview advances a preview exactly once. It is deliberately
// separate from approval creation so a domain owner can attach its own
// approval request transaction and never execute the action here.
func (r *Repository) MarkActionPreview(ctx context.Context, scope tenancy.Scope, actorID, id, status string, now time.Time) error {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(id) == "" || status != "approved" && status != "rejected" && status != "conflict" || !utc(now) {
		return ErrInvalid
	}
	return r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE assistant_action_previews SET status=$5 WHERE organization_id=$1 AND workspace_id=$2 AND actor_id=$3 AND id=$4 AND status='pending' AND expires_at>$6`, scope.OrganizationID().String(), scope.WorkspaceID().String(), actorID, id, status, now)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return ErrConflict
		}
		return nil
	})
}

func digestID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:26]
}

func utc(value time.Time) bool { return !value.IsZero() && value.Location() == time.UTC }
