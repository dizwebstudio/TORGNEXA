// Package operatorassistantrepo persists bounded tenant-scoped assistant
// sessions, runs, normalized answers and typed preview metadata. Raw prompts,
// provider payloads and credentials are never written.
package operatorassistantrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

func utc(value time.Time) bool { return !value.IsZero() && value.Location() == time.UTC }
