// Package mcpaccountsrepo persists tenant-scoped MCP client accounts.
// TokenHash is the only credential material stored; the raw bearer token
// is never persisted.
package mcpaccountsrepo

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

const selectColumns = `id,label,agent_id,model_id,integration_id,permissions,enabled,version,expires_at,COALESCE(rotated_from_id,''),revoked_at,created_at,updated_at`
const selectColumnsAliased = `a.id,a.label,a.agent_id,a.model_id,a.integration_id,a.permissions,a.enabled,a.version,a.expires_at,COALESCE(a.rotated_from_id,''),a.revoked_at,a.created_at,a.updated_at`

func scanAccount(row interface{ Scan(...any) error }) (mcpaccounts.Account, error) {
	return scanAccountFields(row, false)
}

func scanAccountWithActivity(row interface{ Scan(...any) error }) (mcpaccounts.Account, error) {
	return scanAccountFields(row, true)
}

func scanAccountWithToken(row interface{ Scan(...any) error }) (mcpaccounts.Account, []byte, error) {
	var out mcpaccounts.Account
	var permissionsJSON, tokenHash []byte
	var revoked sql.NullTime
	if err := row.Scan(&out.ID, &out.Label, &out.AgentID, &out.ModelID, &out.IntegrationID, &permissionsJSON, &out.Enabled, &out.Version, &out.ExpiresAt, &out.RotatedFromID, &revoked, &out.CreatedAt, &out.UpdatedAt, &tokenHash); err != nil {
		return mcpaccounts.Account{}, nil, err
	}
	if revoked.Valid {
		value := revoked.Time.UTC()
		out.RevokedAt = &value
	}
	out.ExpiresAt, out.CreatedAt, out.UpdatedAt = out.ExpiresAt.UTC(), out.CreatedAt.UTC(), out.UpdatedAt.UTC()
	if err := json.Unmarshal(permissionsJSON, &out.Permissions); err != nil {
		return mcpaccounts.Account{}, nil, fmt.Errorf("mcpaccountsrepo: decode permissions: %w", err)
	}
	return out, tokenHash, nil
}

func scanAccountFields(row interface{ Scan(...any) error }, withActivity bool) (mcpaccounts.Account, error) {
	var out mcpaccounts.Account
	var permissionsJSON []byte
	var revoked sql.NullTime
	var lastUsed sql.NullTime
	destinations := []any{&out.ID, &out.Label, &out.AgentID, &out.ModelID, &out.IntegrationID, &permissionsJSON, &out.Enabled, &out.Version, &out.ExpiresAt, &out.RotatedFromID, &revoked, &out.CreatedAt, &out.UpdatedAt}
	if withActivity {
		destinations = append(destinations, &lastUsed, &out.UseCount)
	}
	if err := row.Scan(destinations...); err != nil {
		return mcpaccounts.Account{}, err
	}
	if revoked.Valid {
		value := revoked.Time.UTC()
		out.RevokedAt = &value
	}
	if lastUsed.Valid {
		value := lastUsed.Time.UTC()
		out.LastUsedAt = &value
	}
	out.ExpiresAt, out.CreatedAt, out.UpdatedAt = out.ExpiresAt.UTC(), out.CreatedAt.UTC(), out.UpdatedAt.UTC()
	if err := json.Unmarshal(permissionsJSON, &out.Permissions); err != nil {
		return mcpaccounts.Account{}, fmt.Errorf("mcpaccountsrepo: decode permissions: %w", err)
	}
	return out, nil
}

func (r *Repository) List(ctx context.Context, scope tenancy.Scope) ([]mcpaccounts.Account, error) {
	var out []mcpaccounts.Account
	err := r.withTx(ctx, true, scope, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+selectColumnsAliased+`,activity.last_used_at,COALESCE(activity.use_count,0) FROM mcp_client_accounts a LEFT JOIN mcp_credential_activity activity ON activity.organization_id=a.organization_id AND activity.workspace_id=a.workspace_id AND activity.account_id=a.id WHERE a.organization_id=$1 AND a.workspace_id=$2 ORDER BY a.created_at,a.id`, scope.OrganizationID().String(), scope.WorkspaceID().String())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			account, err := scanAccountWithActivity(rows)
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
		account, hash, err := scanAccountWithToken(tx.QueryRowContext(ctx, `SELECT `+selectColumns+`,token_hash FROM mcp_client_accounts WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id))
		out, tokenHash = account, hash
		return err
	})
	if err != nil {
		return mcpaccounts.Account{}, nil, err
	}
	return out, tokenHash, nil
}

func (r *Repository) Create(ctx context.Context, scope tenancy.Scope, id string, cmd mcpaccounts.CreateAccount, tokenHash []byte, expiresAt time.Time, rotatedFromID string) (mcpaccounts.Account, error) {
	if strings.TrimSpace(id) == "" || len(tokenHash) != 32 || mcpaccounts.ValidateCreate(cmd) != nil || expiresAt.Before(time.Now().UTC().Add(time.Minute)) {
		return mcpaccounts.Account{}, ErrInvalid
	}
	permissionsJSON, err := json.Marshal(cmd.Permissions)
	if err != nil {
		return mcpaccounts.Account{}, ErrInvalid
	}
	var out mcpaccounts.Account
	err = r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `INSERT INTO mcp_client_accounts(id,organization_id,workspace_id,label,agent_id,model_id,integration_id,permissions,token_hash,expires_at,rotated_from_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,'')) RETURNING `+selectColumns,
			id, scope.OrganizationID().String(), scope.WorkspaceID().String(), strings.TrimSpace(cmd.Label), strings.TrimSpace(cmd.AgentID), strings.TrimSpace(cmd.ModelID), strings.TrimSpace(cmd.IntegrationID), permissionsJSON, tokenHash, expiresAt.UTC(), rotatedFromID)
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
		row := tx.QueryRowContext(ctx, `UPDATE mcp_client_accounts SET enabled=false,revoked_at=COALESCE(revoked_at,clock_timestamp()),version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$4 RETURNING `+selectColumns,
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

// RecordUse updates bounded credential activity after successful constant-time
// token verification. No token, IP address or request body is stored.
func (r *Repository) RecordUse(ctx context.Context, scope tenancy.Scope, id string, at time.Time) error {
	if strings.TrimSpace(id) == "" || at.IsZero() {
		return ErrInvalid
	}
	return r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO mcp_credential_activity(organization_id,workspace_id,account_id,first_used_at,last_used_at,use_count) VALUES($1,$2,$3,$4,$4,1) ON CONFLICT(organization_id,workspace_id,account_id) DO UPDATE SET last_used_at=GREATEST(mcp_credential_activity.last_used_at,EXCLUDED.last_used_at),use_count=mcp_credential_activity.use_count+1`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id, at.UTC())
		if err != nil {
			return err
		}
		rows, _ := result.RowsAffected()
		if rows != 1 {
			return sql.ErrNoRows
		}
		return nil
	})
}

// CreateGoverned commits the account, receipt and security evidence in one
// transaction. A retry returns the original account without credential bytes.
func (r *Repository) CreateGoverned(ctx context.Context, scope tenancy.Scope, id string, cmd mcpaccounts.CreateAccount, tokenHash []byte, expiresAt time.Time, key string, digest []byte, actor, evidenceID string) (mcpaccounts.Account, bool, error) {
	if strings.TrimSpace(id) == "" || len(tokenHash) != 32 || mcpaccounts.ValidateCreate(cmd) != nil || expiresAt.Before(time.Now().UTC().Add(time.Minute)) {
		return mcpaccounts.Account{}, false, ErrInvalid
	}
	permissionsJSON, _ := json.Marshal(cmd.Permissions)
	var out mcpaccounts.Account
	replayed := false
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		resourceID, claimed, err := claimReceipt(ctx, tx, scope, "mcp_account.create", key, digest)
		if err != nil {
			return err
		}
		if !claimed {
			replayed = true
			return loadAccount(ctx, tx, scope, resourceID, &out)
		}
		account, err := scanAccount(tx.QueryRowContext(ctx, `INSERT INTO mcp_client_accounts(id,organization_id,workspace_id,label,agent_id,model_id,integration_id,permissions,token_hash,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+selectColumns, id, scope.OrganizationID().String(), scope.WorkspaceID().String(), strings.TrimSpace(cmd.Label), strings.TrimSpace(cmd.AgentID), strings.TrimSpace(cmd.ModelID), strings.TrimSpace(cmd.IntegrationID), permissionsJSON, tokenHash, expiresAt.UTC()))
		if err != nil {
			return err
		}
		out = account
		if err := writeEvidence(ctx, tx, scope, evidenceID, "mcp.credential.create", actor, out.ID, key, "succeeded", digest, map[string]any{"expires_at": out.ExpiresAt, "permission_count": len(out.Permissions)}); err != nil {
			return err
		}
		return finishReceipt(ctx, tx, scope, "mcp_account.create", key, out.ID)
	})
	return out, replayed, err
}

// RotateGoverned creates a new credential and revokes its predecessor
// atomically. The old token stops authenticating as soon as the transaction
// commits.
func (r *Repository) RotateGoverned(ctx context.Context, scope tenancy.Scope, oldID, newID string, expectedVersion int64, tokenHash []byte, expiresAt time.Time, key string, digest []byte, actor, evidenceID string) (mcpaccounts.Account, bool, error) {
	if oldID == "" || newID == "" || oldID == newID || expectedVersion < 1 || len(tokenHash) != 32 || expiresAt.Before(time.Now().UTC().Add(time.Minute)) {
		return mcpaccounts.Account{}, false, ErrInvalid
	}
	var out mcpaccounts.Account
	replayed := false
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		resourceID, claimed, err := claimReceipt(ctx, tx, scope, "mcp_account.rotate", key, digest)
		if err != nil {
			return err
		}
		if !claimed {
			replayed = true
			return loadAccount(ctx, tx, scope, resourceID, &out)
		}
		old, err := scanAccount(tx.QueryRowContext(ctx, `SELECT `+selectColumns+` FROM mcp_client_accounts WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), oldID))
		if err != nil || !old.Enabled || old.Version != expectedVersion || old.RevokedAt != nil {
			return mcpaccounts.ErrConflict
		}
		permissions, _ := json.Marshal(old.Permissions)
		out, err = scanAccount(tx.QueryRowContext(ctx, `INSERT INTO mcp_client_accounts(id,organization_id,workspace_id,label,agent_id,model_id,integration_id,permissions,token_hash,expires_at,rotated_from_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+selectColumns, newID, scope.OrganizationID().String(), scope.WorkspaceID().String(), old.Label, old.AgentID, old.ModelID, old.IntegrationID, permissions, tokenHash, expiresAt.UTC(), old.ID))
		if err != nil {
			return err
		}
		changed, err := tx.ExecContext(ctx, `UPDATE mcp_client_accounts SET enabled=false,revoked_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$4 AND enabled=true`, scope.OrganizationID().String(), scope.WorkspaceID().String(), old.ID, expectedVersion)
		if err != nil {
			return err
		}
		rows, _ := changed.RowsAffected()
		if rows != 1 {
			return mcpaccounts.ErrConflict
		}
		if err := writeEvidence(ctx, tx, scope, evidenceID, "mcp.credential.rotate", actor, out.ID, key, "rotated", digest, map[string]any{"rotated_from_id": old.ID, "expires_at": out.ExpiresAt}); err != nil {
			return err
		}
		return finishReceipt(ctx, tx, scope, "mcp_account.rotate", key, out.ID)
	})
	return out, replayed, err
}

// RevokeGoverned immediately disables a credential with atomic evidence.
func (r *Repository) RevokeGoverned(ctx context.Context, scope tenancy.Scope, id string, expectedVersion int64, key string, digest []byte, actor, evidenceID string) (mcpaccounts.Account, bool, error) {
	if id == "" || expectedVersion < 1 {
		return mcpaccounts.Account{}, false, ErrInvalid
	}
	var out mcpaccounts.Account
	replayed := false
	err := r.withTx(ctx, false, scope, func(tx *sql.Tx) error {
		resourceID, claimed, err := claimReceipt(ctx, tx, scope, "mcp_account.revoke", key, digest)
		if err != nil {
			return err
		}
		if !claimed {
			replayed = true
			return loadAccount(ctx, tx, scope, resourceID, &out)
		}
		out, err = scanAccount(tx.QueryRowContext(ctx, `UPDATE mcp_client_accounts SET enabled=false,revoked_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$4 AND enabled=true RETURNING `+selectColumns, scope.OrganizationID().String(), scope.WorkspaceID().String(), id, expectedVersion))
		if err != nil {
			return mcpaccounts.ErrConflict
		}
		if err := writeEvidence(ctx, tx, scope, evidenceID, "mcp.credential.revoke", actor, out.ID, key, "revoked", digest, map[string]any{"revoked_at": out.RevokedAt}); err != nil {
			return err
		}
		return finishReceipt(ctx, tx, scope, "mcp_account.revoke", key, out.ID)
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
		return "", false, mcpaccounts.ErrConflict
	}
	return resourceID, false, nil
}

func finishReceipt(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, operation, key, resourceID string) error {
	result, err := tx.ExecContext(ctx, `UPDATE operation_receipts SET state='completed',resource_type='mcp_client_account',resource_id=$5,result=jsonb_build_object('account_id',$5),completed_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND operation=$3 AND idempotency_key=$4 AND state='pending'`, scope.OrganizationID().String(), scope.WorkspaceID().String(), operation, key, resourceID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return mcpaccounts.ErrConflict
	}
	return nil
}

func writeEvidence(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, id, kind, actor, resourceID, correlation, decision string, digest []byte, summary map[string]any) error {
	encoded, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO security_evidence(id,organization_id,workspace_id,evidence_type,actor_ref,resource_type,resource_id,correlation_id,decision,request_sha256,summary) VALUES($1,$2,$3,$4,$5,'mcp_client_account',$6,$7,$8,$9,$10)`, id, scope.OrganizationID().String(), scope.WorkspaceID().String(), kind, actor, resourceID, correlation, decision, digest, encoded)
	return err
}

func loadAccount(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, id string, out *mcpaccounts.Account) error {
	account, err := scanAccount(tx.QueryRowContext(ctx, `SELECT `+selectColumns+` FROM mcp_client_accounts WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id))
	if err == nil {
		*out = account
	}
	return err
}
