// Package ecosystemrepo persists tenant-owned ecosystem onboarding and partner
// certification evidence. Global connector/app catalogs remain owned by their
// existing repositories and checked-in readiness contracts.
package ecosystemrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/torgnexa/torgnexa/internal/core/ecosystem"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

var (
	ErrInvalid  = errors.New("ecosystem repository: invalid input")
	ErrConflict = errors.New("ecosystem repository: conflict")
)

type Repository struct{ db *sql.DB }

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &Repository{db: db}, nil
}

// ListOnboarding returns the latest tenant-scoped onboarding attempts.
func (r *Repository) ListOnboarding(ctx context.Context, scope tenancy.Scope, limit int) ([]ecosystem.OnboardingRun, error) {
	if invalid(r, ctx, scope) || limit < 1 || limit > 200 {
		return nil, ErrInvalid
	}
	items := make([]ecosystem.OnboardingRun, 0, limit)
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT run_id,resource_id,state,checks,owner_ref,idempotency_key,version,created_at,updated_at
FROM ecosystem_onboarding_runs WHERE organization_id=$1 AND workspace_id=$2 ORDER BY updated_at DESC,run_id DESC LIMIT $3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item ecosystem.OnboardingRun
			var checks []byte
			if err := rows.Scan(&item.ID, &item.ResourceID, &item.State, &checks, &item.OwnerRef, &item.IdempotencyKey, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
				return err
			}
			if err := json.Unmarshal(checks, &item.Checks); err != nil {
				return ErrInvalid
			}
			item.CreatedAt, item.UpdatedAt = item.CreatedAt.UTC(), item.UpdatedAt.UTC()
			if err := item.Validate(); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list onboarding: %w", err)
	}
	return items, nil
}

// SaveOnboarding appends a new idempotent onboarding attempt. State changes
// are represented by another attempt, preserving the original evidence.
func (r *Repository) SaveOnboarding(ctx context.Context, scope tenancy.Scope, item ecosystem.OnboardingRun) error {
	if invalid(r, ctx, scope) || item.Validate() != nil {
		return ErrInvalid
	}
	checks, err := json.Marshal(item.Checks)
	if err != nil || len(checks) > 1<<20 {
		return ErrInvalid
	}
	return r.write(ctx, scope, func(tx *sql.Tx) error {
		var inserted string
		err := tx.QueryRowContext(ctx, `INSERT INTO ecosystem_onboarding_runs(organization_id,workspace_id,run_id,resource_id,state,checks,owner_ref,idempotency_key,version,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (organization_id,workspace_id,idempotency_key) DO NOTHING RETURNING run_id`, scope.OrganizationID().String(), scope.WorkspaceID().String(), item.ID, item.ResourceID, item.State, checks, item.OwnerRef, item.IdempotencyKey, item.Version, item.CreatedAt, item.UpdatedAt).Scan(&inserted)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("insert onboarding: %w", err)
		}
		var existing ecosystem.OnboardingRun
		var existingChecks []byte
		if err := tx.QueryRowContext(ctx, `SELECT run_id,resource_id,state,checks,owner_ref,idempotency_key,version,created_at,updated_at FROM ecosystem_onboarding_runs WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), item.IdempotencyKey).Scan(&existing.ID, &existing.ResourceID, &existing.State, &existingChecks, &existing.OwnerRef, &existing.IdempotencyKey, &existing.Version, &existing.CreatedAt, &existing.UpdatedAt); err != nil {
			return fmt.Errorf("read onboarding idempotency: %w", err)
		}
		if json.Unmarshal(existingChecks, &existing.Checks) != nil || !reflect.DeepEqual(existing, item) {
			return ErrConflict
		}
		return nil
	})
}

// ListPartnerCertifications returns tenant-scoped partner evidence without
// exposing credentials, contracts or customer data.
func (r *Repository) ListPartnerCertifications(ctx context.Context, scope tenancy.Scope, limit int) ([]ecosystem.PartnerCertification, error) {
	if invalid(r, ctx, scope) || limit < 1 || limit > 200 {
		return nil, ErrInvalid
	}
	items := make([]ecosystem.PartnerCertification, 0, limit)
	err := r.read(ctx, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT certification_id,partner_ref,tier,state,evidence,expires_at,idempotency_key,version,updated_at
FROM ecosystem_partner_certifications WHERE organization_id=$1 AND workspace_id=$2 ORDER BY updated_at DESC,certification_id DESC LIMIT $3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item ecosystem.PartnerCertification
			var evidence []byte
			if err := rows.Scan(&item.ID, &item.PartnerRef, &item.Tier, &item.State, &evidence, &item.ExpiresAt, &item.IdempotencyKey, &item.Version, &item.UpdatedAt); err != nil {
				return err
			}
			if string(evidence) != "null" && len(evidence) > 0 {
				if err := json.Unmarshal(evidence, &item.Evidence); err != nil {
					return ErrInvalid
				}
			}
			item.ExpiresAt, item.UpdatedAt = item.ExpiresAt.UTC(), item.UpdatedAt.UTC()
			if err := item.Validate(item.UpdatedAt); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list partner certifications: %w", err)
	}
	return items, nil
}

// SavePartnerCertification records certification evidence after the caller's
// approval and qualification checks have completed.
func (r *Repository) SavePartnerCertification(ctx context.Context, scope tenancy.Scope, item ecosystem.PartnerCertification) error {
	if invalid(r, ctx, scope) || item.Validate(item.UpdatedAt) != nil {
		return ErrInvalid
	}
	evidence, err := json.Marshal(item.Evidence)
	if err != nil || len(evidence) > 64<<10 {
		return ErrInvalid
	}
	return r.write(ctx, scope, func(tx *sql.Tx) error {
		var inserted string
		err := tx.QueryRowContext(ctx, `INSERT INTO ecosystem_partner_certifications(organization_id,workspace_id,certification_id,partner_ref,tier,state,evidence,expires_at,idempotency_key,version,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (organization_id,workspace_id,idempotency_key) DO NOTHING RETURNING certification_id`, scope.OrganizationID().String(), scope.WorkspaceID().String(), item.ID, item.PartnerRef, item.Tier, item.State, evidence, item.ExpiresAt, item.IdempotencyKey, item.Version, item.UpdatedAt).Scan(&inserted)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("insert partner certification: %w", err)
		}
		var existing ecosystem.PartnerCertification
		var existingEvidence []byte
		if err := tx.QueryRowContext(ctx, `SELECT certification_id,partner_ref,tier,state,evidence,expires_at,idempotency_key,version,updated_at FROM ecosystem_partner_certifications WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), item.IdempotencyKey).Scan(&existing.ID, &existing.PartnerRef, &existing.Tier, &existing.State, &existingEvidence, &existing.ExpiresAt, &existing.IdempotencyKey, &existing.Version, &existing.UpdatedAt); err != nil {
			return fmt.Errorf("read partner idempotency: %w", err)
		}
		if string(existingEvidence) != "null" && json.Unmarshal(existingEvidence, &existing.Evidence) != nil {
			return ErrConflict
		}
		existing.ExpiresAt, existing.UpdatedAt = existing.ExpiresAt.UTC(), existing.UpdatedAt.UTC()
		if !reflect.DeepEqual(existing, item) {
			return ErrConflict
		}
		return nil
	})
}

func invalid(r *Repository, ctx context.Context, scope tenancy.Scope) bool {
	return r == nil || r.db == nil || ctx == nil || !scope.Valid()
}

func (r *Repository) write(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	if invalid(r, ctx, scope) || fn == nil {
		return ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return fmt.Errorf("scope: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (r *Repository) read(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	if invalid(r, ctx, scope) || fn == nil {
		return ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin read: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return fmt.Errorf("scope read: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
