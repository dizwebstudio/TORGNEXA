package database

import (
	"context"
	"database/sql"
	"errors"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

type transactionKey struct{}
type transactionContext struct {
	pool  *sql.DB
	tx    *sql.Tx
	scope tenancy.Scope
}

// ErrTransactionBoundary rejects attempts to change pool or tenant inside a unit of work.
var ErrTransactionBoundary = errors.New("database: incompatible transaction boundary")

// CheckScope prevents a participating repository from changing the transaction's tenant.
func CheckScope(ctx context.Context, scope tenancy.Scope) error {
	if ctx == nil {
		return ErrTransactionBoundary
	}
	if !scope.Valid() {
		return tenancy.ErrInvalidScope
	}
	if bound, ok := ctx.Value(transactionKey{}).(transactionContext); ok && bound.scope != scope {
		return ErrTransactionBoundary
	}
	return nil
}

// CurrentTransaction returns the caller-owned transaction, or nil for a standalone operation.
// A repository must propagate errors and must never commit the returned transaction.
func CurrentTransaction(ctx context.Context, pool *sql.DB) (*sql.Tx, error) {
	if ctx == nil {
		return nil, ErrTransactionBoundary
	}
	bound, ok := ctx.Value(transactionKey{}).(transactionContext)
	if !ok {
		return nil, nil
	}
	if bound.pool != pool {
		return nil, ErrTransactionBoundary
	}
	return bound.tx, nil
}

// WithinTransaction commits a tenant-scoped unit of work only when its callback succeeds.
// Participating repositories reuse its context synchronously; it must not escape the callback.
func WithinTransaction(ctx context.Context, pool *sql.DB, scope tenancy.Scope, operation func(context.Context) error) error {
	if pool == nil || operation == nil || CheckScope(ctx, scope) != nil {
		return ErrTransactionBoundary
	}
	if tx, err := CurrentTransaction(ctx, pool); err != nil {
		return err
	} else if tx != nil {
		return operation(ctx)
	}
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var org, ws string
	if err := tx.QueryRowContext(ctx, `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &ws); err != nil {
		return err
	}
	if org != scope.OrganizationID().String() || ws != scope.WorkspaceID().String() {
		return ErrTransactionBoundary
	}
	if err := operation(context.WithValue(ctx, transactionKey{}, transactionContext{pool: pool, tx: tx, scope: scope})); err != nil {
		return err
	}
	return tx.Commit()
}
