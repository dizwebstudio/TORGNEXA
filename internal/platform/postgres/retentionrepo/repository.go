// Package retentionrepo implements durable tenant-scoped persistence for Task 061 privacy workflows.
package retentionrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/privacy"
	"github.com/torgnexa/torgnexa/internal/platform/retention"
)

const applyScopeStatement = `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`

type Repository struct{ database *sql.DB }

var _ retention.Repository = (*Repository)(nil)

func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("retention repository: database is required")
	}
	return &Repository{database: database}, nil
}

func (r *Repository) CreateWorkflow(ctx context.Context, scope tenancy.Scope, req *retention.Request, job retention.Job, targets []retention.Target) error {
	if err := validateCall(ctx, scope, r); err != nil {
		return err
	}
	if retention.ValidateJob(scope, job) != nil || (len(targets) == 0 && job.Action != retention.ActionManualReview) {
		return retention.ErrInvalid
	}
	if req != nil && retention.ValidateRequest(scope, *req) != nil {
		return retention.ErrInvalid
	}
	for _, tg := range targets {
		if retention.ValidateTarget(tg) != nil || tg.JobID != job.ID || tg.Action != job.Action {
			return retention.ErrInvalid
		}
	}
	err := r.write(ctx, scope, func(tx *sql.Tx) error {
		if req != nil {
			_, err := tx.ExecContext(ctx, `INSERT INTO privacy_subject_requests (organization_id,workspace_id,request_id,request_type,subject_kind,subject_opaque_id,correction_artifact_ref,status,version,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), req.ID, string(req.Type), req.Subject.Kind, req.Subject.OpaqueID, req.CorrectionArtifactRef, string(req.Status), req.Version, req.CreatedAt.UTC(), req.UpdatedAt.UTC())
			if err != nil {
				return fmt.Errorf("insert subject request: %w", err)
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO privacy_execution_jobs (organization_id,workspace_id,job_id,workflow_kind,request_id,subject_kind,subject_opaque_id,purpose_key,data_class,disposition,action,hold_permitted,status,version,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), job.ID, string(job.Kind), job.RequestID, job.Subject.Kind, job.Subject.OpaqueID, job.PurposeKey, string(job.DataClass), string(job.Disposition), string(job.Action), job.HoldPermitted, string(job.Status), job.Version, job.CreatedAt.UTC(), job.UpdatedAt.UTC())
		if err != nil {
			return fmt.Errorf("insert privacy job: %w", err)
		}
		for _, tg := range targets {
			_, err = tx.ExecContext(ctx, `INSERT INTO privacy_execution_targets (organization_id,workspace_id,job_id,store_name,store_class,action,cursor,status,processed,last_digest,artifact_ref,version,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), tg.JobID, tg.Store, string(tg.Class), string(tg.Action), tg.Cursor, string(tg.Status), tg.Processed, tg.LastDigest, tg.ArtifactRef, tg.Version, tg.UpdatedAt.UTC())
			if err != nil {
				return fmt.Errorf("insert privacy target: %w", err)
			}
		}
		return nil
	})
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return retention.ErrConflict
	}
	return err
}

func (r *Repository) Job(ctx context.Context, scope tenancy.Scope, id string) (retention.Job, error) {
	if err := validateCall(ctx, scope, r); err != nil {
		return retention.Job{}, err
	}
	var j retention.Job
	j.OrganizationID, j.WorkspaceID = scope.OrganizationID(), scope.WorkspaceID()
	var kind, dataClass, disposition, action, status string
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT workflow_kind,request_id,subject_kind,subject_opaque_id,purpose_key,data_class,disposition,action,hold_permitted,status,version,created_at,updated_at FROM privacy_execution_jobs WHERE job_id=$1`, id).Scan(&kind, &j.RequestID, &j.Subject.Kind, &j.Subject.OpaqueID, &j.PurposeKey, &dataClass, &disposition, &action, &j.HoldPermitted, &status, &j.Version, &j.CreatedAt, &j.UpdatedAt)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return retention.Job{}, retention.ErrNotFound
	}
	if err != nil {
		return retention.Job{}, fmt.Errorf("select privacy job: %w", err)
	}
	j.ID = id
	j.Kind = retention.WorkflowKind(kind)
	j.DataClass = privacy.DataClass(dataClass)
	j.Disposition = privacy.Disposition(disposition)
	j.Action = retention.Action(action)
	j.Status = retention.Status(status)
	j.CreatedAt = j.CreatedAt.UTC()
	j.UpdatedAt = j.UpdatedAt.UTC()
	if retention.ValidateJob(scope, j) != nil {
		return retention.Job{}, retention.ErrInvalid
	}
	return j, nil
}

func (r *Repository) Targets(ctx context.Context, scope tenancy.Scope, jobID string) ([]retention.Target, error) {
	if err := validateCall(ctx, scope, r); err != nil {
		return nil, err
	}
	out := []retention.Target{}
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT store_name,store_class,action,cursor,status,processed,last_digest,artifact_ref,version,updated_at FROM privacy_execution_targets WHERE job_id=$1 ORDER BY store_name`, jobID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t retention.Target
			var class, action, status string
			t.JobID = jobID
			if err := rows.Scan(&t.Store, &class, &action, &t.Cursor, &status, &t.Processed, &t.LastDigest, &t.ArtifactRef, &t.Version, &t.UpdatedAt); err != nil {
				return err
			}
			t.Class = retention.StoreClass(class)
			t.Action = retention.Action(action)
			t.Status = retention.TargetStatus(status)
			t.UpdatedAt = t.UpdatedAt.UTC()
			if retention.ValidateTarget(t) != nil {
				return retention.ErrInvalid
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("select privacy targets: %w", err)
	}
	return out, nil
}

func (r *Repository) UpdateJob(ctx context.Context, scope tenancy.Scope, j retention.Job, expected uint64) error {
	if err := validateCall(ctx, scope, r); err != nil {
		return err
	}
	if retention.ValidateJob(scope, j) != nil || expected < 1 || j.Version != expected+1 {
		return retention.ErrInvalid
	}
	return r.write(ctx, scope, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE privacy_execution_jobs SET status=$2,version=$3,updated_at=$4 WHERE job_id=$1 AND version=$5`, j.ID, string(j.Status), j.Version, j.UpdatedAt.UTC(), expected)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return retention.ErrConflict
		}
		return nil
	})
}
func (r *Repository) CommitTargetPage(ctx context.Context, scope tenancy.Scope, t retention.Target, expected uint64, e retention.Evidence) error {
	if err := validateCall(ctx, scope, r); err != nil {
		return err
	}
	if retention.ValidateTarget(t) != nil || retention.ValidateEvidence(e) != nil || expected < 1 || t.Version != expected+1 || e.JobID != t.JobID || e.Store != t.Store || e.Action != t.Action {
		return retention.ErrInvalid
	}
	return r.write(ctx, scope, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE privacy_execution_targets SET cursor=$3,status=$4,processed=$5,last_digest=$6,artifact_ref=$7,version=$8,updated_at=$9 WHERE job_id=$1 AND store_name=$2 AND version=$10`, t.JobID, t.Store, t.Cursor, string(t.Status), t.Processed, t.LastDigest, t.ArtifactRef, t.Version, t.UpdatedAt.UTC(), expected)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return retention.ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO privacy_execution_evidence (organization_id,workspace_id,job_id,store_name,action,cursor_before,cursor_after,processed,digest,artifact_ref,done,recorded_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), e.JobID, e.Store, string(e.Action), e.CursorBefore, e.CursorAfter, e.Processed, e.Digest, e.ArtifactRef, e.Done, e.RecordedAt.UTC())
		return err
	})
}
func (r *Repository) UpdateRequest(ctx context.Context, scope tenancy.Scope, q retention.Request, expected uint64) error {
	if err := validateCall(ctx, scope, r); err != nil {
		return err
	}
	if retention.ValidateRequest(scope, q) != nil || expected < 1 || q.Version != expected+1 {
		return retention.ErrInvalid
	}
	return r.write(ctx, scope, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE privacy_subject_requests SET status=$2,version=$3,updated_at=$4 WHERE request_id=$1 AND version=$5`, q.ID, string(q.Status), q.Version, q.UpdatedAt.UTC(), expected)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return retention.ErrConflict
		}
		return nil
	})
}
func (r *Repository) Request(ctx context.Context, scope tenancy.Scope, id string) (retention.Request, error) {
	if err := validateCall(ctx, scope, r); err != nil {
		return retention.Request{}, err
	}
	var q retention.Request
	q.ID = id
	q.OrganizationID, q.WorkspaceID = scope.OrganizationID(), scope.WorkspaceID()
	var typ, status string
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT request_type,subject_kind,subject_opaque_id,correction_artifact_ref,status,version,created_at,updated_at FROM privacy_subject_requests WHERE request_id=$1`, id).Scan(&typ, &q.Subject.Kind, &q.Subject.OpaqueID, &q.CorrectionArtifactRef, &status, &q.Version, &q.CreatedAt, &q.UpdatedAt)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return retention.Request{}, retention.ErrNotFound
	}
	if err != nil {
		return retention.Request{}, fmt.Errorf("select subject request: %w", err)
	}
	q.Type = retention.RequestType(typ)
	q.Status = retention.Status(status)
	q.CreatedAt = q.CreatedAt.UTC()
	q.UpdatedAt = q.UpdatedAt.UTC()
	if retention.ValidateRequest(scope, q) != nil {
		return retention.Request{}, retention.ErrInvalid
	}
	return q, nil
}
func (r *Repository) PlaceHold(ctx context.Context, scope tenancy.Scope, h retention.LegalHold) error {
	if err := validateCall(ctx, scope, r); err != nil {
		return err
	}
	if retention.ValidateLegalHold(scope, h) != nil {
		return retention.ErrInvalid
	}
	var exp any
	if h.ExpiresAt != nil {
		exp = h.ExpiresAt.UTC()
	}
	return r.write(ctx, scope, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO privacy_legal_holds (organization_id,workspace_id,hold_id,selector_kind,subject_kind,subject_opaque_id,purpose_key,data_class,reason_ref,expires_at,released_at,version,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULL,$11,$12)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), h.ID, string(h.SelectorKind), h.Subject.Kind, h.Subject.OpaqueID, h.PurposeKey, string(h.DataClass), h.ReasonRef, exp, h.Version, h.CreatedAt.UTC())
		return err
	})
}
func (r *Repository) ActiveHolds(ctx context.Context, scope tenancy.Scope, now time.Time) ([]retention.LegalHold, error) {
	if err := validateCall(ctx, scope, r); err != nil {
		return nil, err
	}
	out := []retention.LegalHold{}
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT hold_id,selector_kind,subject_kind,subject_opaque_id,purpose_key,data_class,reason_ref,expires_at,released_at,version,created_at FROM privacy_legal_holds WHERE released_at IS NULL AND (expires_at IS NULL OR expires_at>$1) ORDER BY hold_id`, now.UTC())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h retention.LegalHold
			var sk, dc string
			var exp, rel sql.NullTime
			h.OrganizationID, h.WorkspaceID = scope.OrganizationID(), scope.WorkspaceID()
			if err := rows.Scan(&h.ID, &sk, &h.Subject.Kind, &h.Subject.OpaqueID, &h.PurposeKey, &dc, &h.ReasonRef, &exp, &rel, &h.Version, &h.CreatedAt); err != nil {
				return err
			}
			h.SelectorKind = retention.HoldSelectorKind(sk)
			h.DataClass = privacy.DataClass(dc)
			if exp.Valid {
				x := exp.Time.UTC()
				h.ExpiresAt = &x
			}
			if rel.Valid {
				x := rel.Time.UTC()
				h.ReleasedAt = &x
			}
			h.CreatedAt = h.CreatedAt.UTC()
			if retention.ValidateLegalHold(scope, h) != nil {
				return retention.ErrInvalid
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("select legal holds: %w", err)
	}
	return out, nil
}
func (r *Repository) ReleaseHold(ctx context.Context, scope tenancy.Scope, id string, expected uint64, at time.Time) error {
	if err := validateCall(ctx, scope, r); err != nil {
		return err
	}
	if expected < 1 || at.IsZero() {
		return retention.ErrInvalid
	}
	return r.write(ctx, scope, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE privacy_legal_holds SET released_at=$2,version=version+1 WHERE hold_id=$1 AND version=$3 AND released_at IS NULL`, id, at.UTC(), expected)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return retention.ErrConflict
		}
		return nil
	})
}

func validateCall(ctx context.Context, scope tenancy.Scope, r *Repository) error {
	if ctx == nil {
		return retention.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.database == nil {
		return retention.ErrInvalid
	}
	if !scope.Valid() {
		return tenancy.ErrInvalidScope
	}
	return nil
}
func (r *Repository) read(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	return r.transaction(ctx, scope, &sql.TxOptions{ReadOnly: true}, fn)
}
func (r *Repository) write(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	return r.transaction(ctx, scope, nil, fn)
}
func (r *Repository) transaction(ctx context.Context, scope tenancy.Scope, opt *sql.TxOptions, fn func(*sql.Tx) error) error {
	tx, err := r.database.BeginTx(ctx, opt)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, applyScopeStatement, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
