// Package connectorconfigrepo stores host-owned non-secret connector runtime configuration.
package connectorconfigrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var (
	ErrNotFound = errors.New("connector runtime config: not found")
	ErrInvalid  = errors.New("connector runtime config: invalid")
	ErrConflict = errors.New("connector runtime config: conflict")
)

type Repository struct{ database *sql.DB }

// State is the non-secret projection used by read models. Config bytes never
// leave this package through the bulk boundary.
type State struct {
	Present bool
	Valid   bool
	Version int64
}

func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, ErrInvalid
	}
	return &Repository{database: database}, nil
}

func (r *Repository) Config(ctx context.Context, scope tenancy.Scope, accountID string) (json.RawMessage, int64, error) {
	if ctx == nil || !scope.Valid() || r == nil || r.database == nil || accountID == "" {
		return nil, 0, ErrInvalid
	}
	var raw []byte
	var version int64
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT config, version FROM connector_runtime_configs WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID).Scan(&raw, &version)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	if !json.Valid(raw) || len(raw) < 2 || len(raw) > 32768 || version < 1 {
		return nil, 0, ErrInvalid
	}
	return append(json.RawMessage(nil), raw...), version, nil
}

// States returns presence/validity/version for up to 100 accounts in one
// tenant-scoped query. It deliberately discards configuration payloads after
// validation, so callers cannot accidentally expose non-secret config values.
func (r *Repository) States(ctx context.Context, scope tenancy.Scope, accountIDs []string) (map[string]State, error) {
	if ctx == nil || !scope.Valid() || r == nil || r.database == nil || len(accountIDs) > 101 {
		return nil, ErrInvalid
	}
	result := make(map[string]State, len(accountIDs))
	if len(accountIDs) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(accountIDs))
	args := make([]any, 0, len(accountIDs)+2)
	args = append(args, scope.OrganizationID().String(), scope.WorkspaceID().String())
	for i, accountID := range accountIDs {
		if accountID == "" || len(accountID) > 128 || strings.ContainsAny(accountID, "\x00\r\n") {
			return nil, ErrInvalid
		}
		placeholders[i] = fmt.Sprintf("$%d", i+3)
		args = append(args, accountID)
	}
	query := `SELECT connector_account_id,config,version FROM connector_runtime_configs
WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id IN (` + strings.Join(placeholders, ",") + `)`
	err := r.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var accountID string
			var raw []byte
			var version int64
			if err := rows.Scan(&accountID, &raw, &version); err != nil {
				return err
			}
			result[accountID] = State{Present: true, Valid: version >= 1 && validateConfig(raw) == nil, Version: version}
		}
		return rows.Err()
	})
	return result, err
}

func (r *Repository) Put(ctx context.Context, scope tenancy.Scope, accountID string, config json.RawMessage, expectedVersion int64) (int64, error) {
	if ctx == nil || !scope.Valid() || r == nil || r.database == nil || accountID == "" || validateConfig(config) != nil || expectedVersion < 0 {
		return 0, ErrInvalid
	}
	var version int64
	err := r.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		if expectedVersion == 0 {
			return tx.QueryRowContext(ctx, `INSERT INTO connector_runtime_configs(organization_id,workspace_id,connector_account_id,config) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING RETURNING version`, scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID, []byte(config)).Scan(&version)
		}
		return tx.QueryRowContext(ctx, `UPDATE connector_runtime_configs SET config=$4,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3 AND version=$5 RETURNING version`, scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID, []byte(config), expectedVersion).Scan(&version)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrConflict
	}
	if err != nil {
		return 0, err
	}
	return version, nil
}

func (r *Repository) withTx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("connector runtime config: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var org, ws string
	if err := tx.QueryRowContext(ctx, `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &ws); err != nil {
		return fmt.Errorf("connector runtime config: scope: %w", err)
	}
	if org != scope.OrganizationID().String() || ws != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("connector runtime config: commit: %w", err)
	}
	return nil
}

func validateConfig(raw json.RawMessage) error {
	if !json.Valid(raw) || len(raw) < 2 || len(raw) > 32768 {
		return ErrInvalid
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return ErrInvalid
	}
	root, ok := value.(map[string]any)
	if !ok || len(root) == 0 || containsSensitiveKey(root) {
		return ErrInvalid
	}
	return nil
}

func containsSensitiveKey(value any) bool {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			normalized := strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(key)))
			compact := strings.ReplaceAll(normalized, "_", "")
			if strings.Contains(compact, "password") || strings.Contains(compact, "secret") || strings.Contains(compact, "token") || strings.Contains(compact, "apikey") || strings.Contains(compact, "accesskey") || strings.Contains(compact, "consumerkey") || strings.Contains(compact, "consumersecret") || strings.Contains(compact, "privatekey") || strings.Contains(compact, "authorization") {
				return true
			}
			if containsSensitiveKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range current {
			if containsSensitiveKey(child) {
				return true
			}
		}
	}
	return false
}
