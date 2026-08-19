// Package settlementrepo provides tenant-scoped PostgreSQL access to the
// append-only marketplace settlement ledger.
package settlementrepo

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/settlements"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

// Repository reads the canonical settlement ledger from PostgreSQL.
type Repository struct{ db *sql.DB }

// New constructs a settlement ledger repository.
func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, settlements.ErrInvalid
	}
	return &Repository{db: db}, nil
}

// List returns a stable, bounded page ordered by entry identifier.
func (r *Repository) List(ctx context.Context, scope tenancy.Scope, afterID string, limit int) ([]settlements.Entry, error) {
	if ctx == nil || r == nil || r.db == nil || !scope.Valid() || len(afterID) > 128 || limit < 1 || limit > 201 {
		return nil, settlements.ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("settlement repository: begin: %w", err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); err != nil {
		return nil, fmt.Errorf("settlement repository: scope: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT entry_id,provider,provider_account_id,provider_entry_ref,order_id,adjusts_entry_id,fee_code,fx_rate_ref,kind,amount_minor,currency,occurred_at,imported_at,disputed
FROM settlement_entries WHERE organization_id=$1 AND workspace_id=$2 AND entry_id>$3 ORDER BY entry_id LIMIT $4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("settlement repository: list: %w", err)
	}
	defer rows.Close()
	items := make([]settlements.Entry, 0, limit)
	for rows.Next() {
		var item settlements.Entry
		var kind, currency string
		var minor int64
		if err := rows.Scan(&item.ID, &item.SourceSystem, &item.SourceAccountID, &item.SourceEntryRef, &item.OrderID, &item.AdjustsEntryID, &item.FeeCode, &item.FXRateRef, &kind, &minor, &currency, &item.OccurredAt, &item.ImportedAt, &item.Disputed); err != nil {
			return nil, fmt.Errorf("settlement repository: scan: %w", err)
		}
		code, err := domain.NewCurrency(currency)
		if err != nil {
			return nil, settlements.ErrInvalid
		}
		item.Amount, err = domain.NewMoney(minor, code)
		if err != nil {
			return nil, settlements.ErrInvalid
		}
		item.Kind = settlements.Kind(kind)
		item.OccurredAt, item.ImportedAt = item.OccurredAt.UTC(), item.ImportedAt.UTC()
		if item.Validate() != nil {
			return nil, settlements.ErrInvalid
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settlement repository: rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("settlement repository: commit: %w", err)
	}
	return items, nil
}
