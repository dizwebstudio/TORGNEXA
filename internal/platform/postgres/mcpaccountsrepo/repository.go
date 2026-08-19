// Package mcpaccountsrepo persists tenant-scoped MCP client accounts.
// TokenHash is the only credential material stored; the raw bearer token
// is never persisted.
package mcpaccountsrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/mcpaccounts"
)

var ErrInvalid = errors.New("mcpaccountsrepo: invalid")

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
		return fmt.Errorf("mcpaccountsrepo: scope: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

const selectColumns = `id,label,agent_id,model_id,integration_id,permissions,enabled,version,created_at,updated_at`

func scanAccount(row interface{ Scan(...any) error }) (mcpaccounts.Account, error) {
	var out mcpaccounts.Account
	var permissionsJSON []byte
	if err := row.Scan(&out.ID, &out.Label, &out.AgentID, &out.ModelID, &out.IntegrationID, &permissionsJSON, &out.Enabled, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return mcpaccounts.Account{}, err
	}
	if err := json.Unmarshal(permissionsJSON, &out.Permissions); err != nil {
		return mcpaccounts.Account{}, fmt.Errorf("mcpaccountsrepo: decode permissions: %w", err)
	}
	return out, nil
}

func (r *Repository) List(ctx context.Context, scope tenancy.Scope) ([]mcpaccounts.Account, error) {
	var out []mcpaccounts.Account
	err := r.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+selectColumns+` FROM mcp_client_accounts WHERE organization_id=$1 AND workspace_id=$2 ORDER BY created_at, id`, scope.OrganizationID().String(), scope.WorkspaceID().String())
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

// FindByID is used only by the MCP identity resolver, after it has already
// parsed organization/workspace/account IDs out of the presented bearer
// token and built scope itself. It is still a normal tenant-scoped,
// RLS-enforced lookup by primary key: the embedded token IDs are only a
// routing hint, never trusted on their own.
func (r *Repository) FindByID(ctx context.Context, scope tenancy.Scope, id string) (mcpaccounts.Account, []byte, error) {
	if strings.TrimSpace(id) == "" {
		return mcpaccounts.Account{}, nil, ErrInvalid
	}
	var out mcpaccounts.Account
	var tokenHash []byte
	err := r.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `SELECT `+selectColumns+`,token_hash FROM mcp_client_accounts WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`,
			scope.OrganizationID().String(), scope.WorkspaceID().String(), id)
		var permissionsJSON []byte
		if err := row.Scan(&out.ID, &out.Label, &out.AgentID, &out.ModelID, &out.IntegrationID, &permissionsJSON, &out.Enabled, &out.Version, &out.CreatedAt, &out.UpdatedAt, &tokenHash); err != nil {
			return err
		}
		return json.Unmarshal(permissionsJSON, &out.Permissions)
	})
	if err != nil {
		return mcpaccounts.Account{}, nil, err
	}
	return out, tokenHash, nil
}

func (r *Repository) Create(ctx context.Context, scope tenancy.Scope, id string, cmd mcpaccounts.CreateAccount, tokenHash []byte) (mcpaccounts.Account, error) {
	if strings.TrimSpace(id) == "" || len(tokenHash) != 32 || mcpaccounts.ValidateCreate(cmd) != nil {
		return mcpaccounts.Account{}, ErrInvalid
	}
	permissionsJSON, err := json.Marshal(cmd.Permissions)
	if err != nil {
		return mcpaccounts.Account{}, ErrInvalid
	}
	var out mcpaccounts.Account
	err = r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `INSERT INTO mcp_client_accounts(id,organization_id,workspace_id,label,agent_id,model_id,integration_id,permissions,token_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+selectColumns,
			id, scope.OrganizationID().String(), scope.WorkspaceID().String(), strings.TrimSpace(cmd.Label), strings.TrimSpace(cmd.AgentID), strings.TrimSpace(cmd.ModelID), strings.TrimSpace(cmd.IntegrationID), permissionsJSON, tokenHash)
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
// deleted (DELETE is revoked at the schema level) so audit history stays
// intact.
func (r *Repository) Disable(ctx context.Context, scope tenancy.Scope, id string, expectedVersion int64) (mcpaccounts.Account, error) {
	if strings.TrimSpace(id) == "" || expectedVersion < 1 {
		return mcpaccounts.Account{}, ErrInvalid
	}
	var out mcpaccounts.Account
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `UPDATE mcp_client_accounts SET enabled=false,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$4 RETURNING `+selectColumns,
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
