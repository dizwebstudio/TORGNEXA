// Package aiadvisoryrepo persists tenant-scoped configured AI provider
// accounts. Credential material is never stored here: only the
// secrets.Reference returned by the secrets provider is kept.
package aiadvisoryrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

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
