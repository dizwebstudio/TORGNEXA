// Package aiadvisoryrepo persists tenant-scoped configured AI provider
// accounts. Credential material is never stored here: only the
// secrets.Reference returned by the secrets provider is kept.
package aiadvisoryrepo

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
	"github.com/torgnexa/torgnexa/internal/platform/aiadvisory"
)

var ErrInvalid = errors.New("aiadvisoryrepo: invalid")

type Repository struct{ db *sql.DB }

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &Repository{db: db}, nil
}

func (r *Repository) withTx(ctx context.Context, readOnly bool, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	if !scope.Valid() {
		return ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var org, ws string
	if err := tx.QueryRowContext(ctx, `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &ws); err != nil {
		return fmt.Errorf("aiadvisoryrepo: scope: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

const selectColumns = `id,provider,label,model,base_url,folder_id,secret_reference,enabled,version,created_at,updated_at`

func scanAccount(row interface{ Scan(...any) error }) (aiadvisory.Account, error) {
	var out aiadvisory.Account
	var provider string
	if err := row.Scan(&out.ID, &provider, &out.Label, &out.Model, &out.BaseURL, &out.FolderID, &out.SecretReference, &out.Enabled, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return aiadvisory.Account{}, err
	}
	out.Provider = aiadvisory.Provider(provider)
	return out, nil
}

func (r *Repository) List(ctx context.Context, scope tenancy.Scope) ([]aiadvisory.Account, error) {
	var out []aiadvisory.Account
	err := r.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+selectColumns+` FROM ai_provider_accounts WHERE organization_id=$1 AND workspace_id=$2 ORDER BY created_at, id`, scope.OrganizationID().String(), scope.WorkspaceID().String())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			account, err := scanAccount(rows)
			if err != nil {
				return err
			}
			out = append(out, account)
		}
		return rows.Err()
	})
	return out, err
}

func (r *Repository) Get(ctx context.Context, scope tenancy.Scope, id string) (aiadvisory.Account, error) {
	if strings.TrimSpace(id) == "" {
		return aiadvisory.Account{}, ErrInvalid
	}
	var out aiadvisory.Account
	err := r.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `SELECT `+selectColumns+` FROM ai_provider_accounts WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id)
		account, err := scanAccount(row)
		if err != nil {
			return err
		}
		out = account
		return nil
	})
	return out, err
}

func (r *Repository) Create(ctx context.Context, scope tenancy.Scope, id string, cmd aiadvisory.CreateAccount, secretReference string) (aiadvisory.Account, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(secretReference) == "" || aiadvisory.ValidateCreate(cmd) != nil {
		return aiadvisory.Account{}, ErrInvalid
	}
	var out aiadvisory.Account
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `INSERT INTO ai_provider_accounts(id,organization_id,workspace_id,provider,label,model,base_url,folder_id,secret_reference) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+selectColumns,
			id, scope.OrganizationID().String(), scope.WorkspaceID().String(), string(cmd.Provider), strings.TrimSpace(cmd.Label), strings.TrimSpace(cmd.Model), cmd.BaseURL, strings.TrimSpace(cmd.FolderID), secretReference)
		account, err := scanAccount(row)
		if err != nil {
			return err
		}
		out = account
		return nil
	})
	return out, err
}

// Disable soft-deletes the account (enabled=false); rows are never hard
// deleted (DELETE is revoked at the schema level) so audit/reference history
// stays intact.
func (r *Repository) Disable(ctx context.Context, scope tenancy.Scope, id string, expectedVersion int64) (aiadvisory.Account, error) {
	if strings.TrimSpace(id) == "" || expectedVersion < 1 {
		return aiadvisory.Account{}, ErrInvalid
	}
	var out aiadvisory.Account
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `UPDATE ai_provider_accounts SET enabled=false,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$4 RETURNING `+selectColumns,
			scope.OrganizationID().String(), scope.WorkspaceID().String(), id, expectedVersion)
		account, err := scanAccount(row)
		if err != nil {
			return err
		}
		out = account
		return nil
	})
	return out, err
}

// CreateGoverned commits the provider-account reference, receipt and
// minimized security evidence atomically. Raw credential material never enters
// this repository.
func (r *Repository) CreateGoverned(ctx context.Context, scope tenancy.Scope, id string, cmd aiadvisory.CreateAccount, secretReference, key string, digest []byte, actor, evidenceID string) (aiadvisory.Account, bool, error) {
	if id == "" || secretReference == "" || aiadvisory.ValidateCreate(cmd) != nil {
		return aiadvisory.Account{}, false, ErrInvalid
	}
	var out aiadvisory.Account
	replayed := false
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		resourceID, claimed, err := claimReceipt(ctx, tx, scope, "ai_provider_account.create", key, digest)
		if err != nil {
			return err
		}
		if !claimed {
			replayed = true
			return loadAccount(ctx, tx, scope, resourceID, &out)
		}
		out, err = scanAccount(tx.QueryRowContext(ctx, `INSERT INTO ai_provider_accounts(id,organization_id,workspace_id,provider,label,model,base_url,folder_id,secret_reference) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+selectColumns, id, scope.OrganizationID().String(), scope.WorkspaceID().String(), string(cmd.Provider), strings.TrimSpace(cmd.Label), strings.TrimSpace(cmd.Model), cmd.BaseURL, strings.TrimSpace(cmd.FolderID), secretReference))
		if err != nil {
			return err
		}
		if err := writeEvidence(ctx, tx, scope, evidenceID, "ai.provider.create", actor, out.ID, key, "succeeded", digest, map[string]any{"provider": out.Provider, "model": out.Model}); err != nil {
			return err
		}
		return finishReceipt(ctx, tx, scope, "ai_provider_account.create", key, out.ID)
	})
	return out, replayed, err
}

// DisableGoverned disables a provider account with atomic receipt/evidence.
func (r *Repository) DisableGoverned(ctx context.Context, scope tenancy.Scope, id string, expectedVersion int64, key string, digest []byte, actor, evidenceID string) (aiadvisory.Account, bool, error) {
	if id == "" || expectedVersion < 1 {
		return aiadvisory.Account{}, false, ErrInvalid
	}
	var out aiadvisory.Account
	replayed := false
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		resourceID, claimed, err := claimReceipt(ctx, tx, scope, "ai_provider_account.disable", key, digest)
		if err != nil {
			return err
		}
		if !claimed {
			replayed = true
			return loadAccount(ctx, tx, scope, resourceID, &out)
		}
		out, err = scanAccount(tx.QueryRowContext(ctx, `UPDATE ai_provider_accounts SET enabled=false,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$4 AND enabled=true RETURNING `+selectColumns, scope.OrganizationID().String(), scope.WorkspaceID().String(), id, expectedVersion))
		if err != nil {
			return aiadvisory.ErrConflict
		}
		if err := writeEvidence(ctx, tx, scope, evidenceID, "ai.provider.disable", actor, out.ID, key, "revoked", digest, map[string]any{"provider": out.Provider}); err != nil {
			return err
		}
		return finishReceipt(ctx, tx, scope, "ai_provider_account.disable", key, out.ID)
	})
	return out, replayed, err
}

func claimReceipt(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, operation, key string, digest []byte) (string, bool, error) {
	if key == "" || len(key) > 128 || len(digest) != 32 {
		return "", false, ErrInvalid
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO operation_receipts(organization_id,workspace_id,operation,idempotency_key,request_sha256,state) VALUES($1,$2,$3,$4,$5,'pending') ON CONFLICT DO NOTHING`, scope.OrganizationID().String(), scope.WorkspaceID().String(), operation, key, digest)
	if err != nil {
		return "", false, err
	}
	rows, _ := result.RowsAffected()
	if rows == 1 {
		return "", true, nil
	}
	var stored []byte
	var state, resourceID string
	if err := tx.QueryRowContext(ctx, `SELECT request_sha256,state,resource_id FROM operation_receipts WHERE organization_id=$1 AND workspace_id=$2 AND operation=$3 AND idempotency_key=$4 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), operation, key).Scan(&stored, &state, &resourceID); err != nil {
		return "", false, err
	}
	if !bytes.Equal(stored, digest) || state != "completed" || resourceID == "" {
		return "", false, aiadvisory.ErrConflict
	}
	return resourceID, false, nil
}

func finishReceipt(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, operation, key, resourceID string) error {
	result, err := tx.ExecContext(ctx, `UPDATE operation_receipts SET state='completed',resource_type='ai_provider_account',resource_id=$5,result=jsonb_build_object('account_id',$5),completed_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND operation=$3 AND idempotency_key=$4 AND state='pending'`, scope.OrganizationID().String(), scope.WorkspaceID().String(), operation, key, resourceID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return aiadvisory.ErrConflict
	}
	return nil
}

func writeEvidence(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, id, kind, actor, resourceID, correlation, decision string, digest []byte, summary map[string]any) error {
	encoded, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO security_evidence(id,organization_id,workspace_id,evidence_type,actor_ref,resource_type,resource_id,correlation_id,decision,request_sha256,summary,occurred_at) VALUES($1,$2,$3,$4,$5,'ai_provider_account',$6,$7,$8,$9,$10,$11)`, id, scope.OrganizationID().String(), scope.WorkspaceID().String(), kind, actor, resourceID, correlation, decision, digest, encoded, time.Now().UTC())
	return err
}

func loadAccount(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, id string, out *aiadvisory.Account) error {
	account, err := scanAccount(tx.QueryRowContext(ctx, `SELECT `+selectColumns+` FROM ai_provider_accounts WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id))
	if err == nil {
		*out = account
	}
	return err
}
