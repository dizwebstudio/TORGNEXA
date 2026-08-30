// Package workerrepo owns the narrow cross-tenant dispatch boundary used by
// the production worker. Returned metadata is limited to tenant scope, kind,
// item ID and lease data; callers must re-apply tenant RLS for all domain IO.
package workerrepo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var ErrSchemaUnavailable = errors.New("worker repository: runtime schema unavailable")

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
var errorCode = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

type Kind string

const (
	KindReconciliation    Kind = "reconciliation"
	KindUpload            Kind = "upload"
	KindPrivacy           Kind = "privacy"
	KindWarehouseIncident Kind = "warehouse_incident"
	KindSocialPublication Kind = "social_publication"
)

func (k Kind) Valid() bool {
	return k == KindReconciliation || k == KindUpload || k == KindPrivacy || k == KindWarehouseIncident || k == KindSocialPublication
}

type Job struct {
	Kind         Kind
	Scope        tenancy.Scope
	ItemID       string
	LeaseToken   string
	LeaseUntil   time.Time
	AttemptCount int
}

type Repository struct {
	db     *sql.DB
	random func([]byte) (int, error)
}

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("worker repository: database required")
	}
	return &Repository{db: db, random: rand.Read}, nil
}

func (r *Repository) ActiveScopes(ctx context.Context, limit int) ([]tenancy.Scope, error) {
	if ctx == nil || r == nil || r.db == nil || limit < 1 || limit > 1000 {
		return nil, errors.New("worker repository: invalid scope request")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT organization_id,workspace_id FROM list_worker_active_scopes($1)`, limit)
	if err != nil {
		return nil, fmt.Errorf("worker repository: list active scopes: %w", normalizeSchemaError(err))
	}
	defer rows.Close()
	out := make([]tenancy.Scope, 0, limit)
	for rows.Next() {
		var org, ws string
		if err := rows.Scan(&org, &ws); err != nil {
			return nil, fmt.Errorf("worker repository: scan scope: %w", err)
		}
		scope, err := tenancy.ParseScope(org, ws)
		if err != nil {
			return nil, errors.New("worker repository: invalid persisted scope")
		}
		out = append(out, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("worker repository: iterate scopes: %w", err)
	}
	return out, nil
}

// PaymentScopes returns tenant scopes that currently have at least one active
// payment connector account. The database function exposes scope identities
// only; callers must re-enter each scope before reading payment or account
// data, preserving the same RLS boundary as ActiveScopes.
func (r *Repository) PaymentScopes(ctx context.Context, limit int) ([]tenancy.Scope, error) {
	if ctx == nil || r == nil || r.db == nil || limit < 1 || limit > 1000 {
		return nil, errors.New("worker repository: invalid payment scope request")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT organization_id,workspace_id FROM list_worker_payment_scopes($1)`, limit)
	if err != nil {
		return nil, fmt.Errorf("worker repository: list payment scopes: %w", normalizeSchemaError(err))
	}
	defer rows.Close()
	out := make([]tenancy.Scope, 0, limit)
	for rows.Next() {
		var org, ws string
		if err := rows.Scan(&org, &ws); err != nil {
			return nil, fmt.Errorf("worker repository: scan payment scope: %w", err)
		}
		scope, err := tenancy.ParseScope(org, ws)
		if err != nil {
			return nil, errors.New("worker repository: invalid persisted payment scope")
		}
		out = append(out, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("worker repository: iterate payment scopes: %w", err)
	}
	return out, nil
}

func (r *Repository) Claim(ctx context.Context, kind Kind, workerID string, batch int, lease time.Duration) ([]Job, error) {
	if ctx == nil || r == nil || r.db == nil || !kind.Valid() || !safeID.MatchString(workerID) || batch < 1 || batch > 1000 || lease < 10*time.Second || lease > 10*time.Minute {
		return nil, errors.New("worker repository: invalid claim")
	}
	token, err := r.token()
	if err != nil {
		return nil, fmt.Errorf("worker repository: lease token: %w", err)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT kind,organization_id,workspace_id,item_id,lease_token,lease_until,attempt_count FROM claim_worker_runtime_jobs($1,$2,$3,$4,$5)`, string(kind), workerID, token, batch, int(lease/time.Second))
	if err != nil {
		normalized := normalizeSchemaError(err)
		var state sqlStateError
		if (kind == KindPrivacy || kind == KindWarehouseIncident || kind == KindSocialPublication) && errors.As(err, &state) && state.SQLState() == "22023" {
			normalized = ErrSchemaUnavailable // rolling upgrade: older function does not know this job kind
		}
		return nil, fmt.Errorf("worker repository: claim %s: %w", kind, normalized)
	}
	defer rows.Close()
	jobs := make([]Job, 0, batch)
	for rows.Next() {
		var kindText, org, ws, item, leaseToken string
		var until time.Time
		var attempts int
		if err := rows.Scan(&kindText, &org, &ws, &item, &leaseToken, &until, &attempts); err != nil {
			return nil, fmt.Errorf("worker repository: scan claim: %w", err)
		}
		scope, err := tenancy.ParseScope(org, ws)
		if err != nil || Kind(kindText) != kind || !safeID.MatchString(item) || !safeID.MatchString(leaseToken) || until.IsZero() || attempts < 1 {
			return nil, errors.New("worker repository: invalid claimed job")
		}
		jobs = append(jobs, Job{Kind: kind, Scope: scope, ItemID: item, LeaseToken: leaseToken, LeaseUntil: until.UTC(), AttemptCount: attempts})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("worker repository: iterate claims: %w", err)
	}
	return jobs, nil
}

func (r *Repository) Release(ctx context.Context, job Job, delay time.Duration, code string) error {
	if err := validateJob(job); err != nil || ctx == nil || delay < 0 || delay > 24*time.Hour || (code != "" && !errorCode.MatchString(code)) {
		return errors.New("worker repository: invalid release")
	}
	var changed bool
	var nullable any
	if code != "" {
		nullable = code
	}
	err := r.db.QueryRowContext(ctx, `SELECT release_worker_runtime_job($1,$2,$3,$4,$5,$6,$7)`, string(job.Kind), job.Scope.OrganizationID().String(), job.Scope.WorkspaceID().String(), job.ItemID, job.LeaseToken, int(delay/time.Second), nullable).Scan(&changed)
	if err != nil {
		return fmt.Errorf("worker repository: release: %w", normalizeSchemaError(err))
	}
	if !changed {
		return errors.New("worker repository: stale lease")
	}
	return nil
}

func (r *Repository) Complete(ctx context.Context, job Job) error {
	if err := validateJob(job); err != nil || ctx == nil {
		return errors.New("worker repository: invalid completion")
	}
	var changed bool
	if err := r.db.QueryRowContext(ctx, `SELECT complete_worker_runtime_job($1,$2,$3,$4,$5)`, string(job.Kind), job.Scope.OrganizationID().String(), job.Scope.WorkspaceID().String(), job.ItemID, job.LeaseToken).Scan(&changed); err != nil {
		return fmt.Errorf("worker repository: complete: %w", normalizeSchemaError(err))
	}
	if !changed {
		return errors.New("worker repository: stale lease")
	}
	return nil
}

type sqlStateError interface{ SQLState() string }

func normalizeSchemaError(err error) error {
	if err == nil {
		return nil
	}
	var state sqlStateError
	if errors.As(err, &state) {
		switch state.SQLState() {
		case "42883", "42P01": // undefined_function, undefined_table during expand rollout
			return fmt.Errorf("%w: %v", ErrSchemaUnavailable, err)
		}
	}
	return err
}

func (r *Repository) token() (string, error) {
	raw := make([]byte, 16)
	if _, err := r.random(raw); err != nil {
		return "", err
	}
	return "lease_" + hex.EncodeToString(raw), nil
}

func validateJob(job Job) error {
	if !job.Kind.Valid() || !job.Scope.Valid() || !safeID.MatchString(job.ItemID) || !safeID.MatchString(job.LeaseToken) || job.LeaseUntil.IsZero() || job.AttemptCount < 1 {
		return errors.New("invalid job")
	}
	return nil
}
