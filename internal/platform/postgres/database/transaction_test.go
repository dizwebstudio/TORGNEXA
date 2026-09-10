package database

import (
	"context"
	"database/sql"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"testing"
)

func TestTransactionContextRejectsPoolAndScopeChanges(t *testing.T) {
	scope, _ := tenancy.ParseScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	other, _ := tenancy.ParseScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000003")
	pool, tx := &sql.DB{}, &sql.Tx{}
	ctx := context.WithValue(context.Background(), transactionKey{}, transactionContext{pool: pool, tx: tx, scope: scope})
	if got, err := CurrentTransaction(ctx, pool); err != nil || got != tx {
		t.Fatal("transaction not reused", err)
	}
	if _, err := CurrentTransaction(ctx, &sql.DB{}); !errors.Is(err, ErrTransactionBoundary) {
		t.Fatal("cross-pool transaction accepted")
	}
	if err := CheckScope(ctx, other); !errors.Is(err, ErrTransactionBoundary) {
		t.Fatal("cross-workspace transaction accepted")
	}
	if err := CheckScope(ctx, scope); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := WithinTransaction(ctx, pool, other, func(context.Context) error { called = true; return nil }); err == nil || called {
		t.Fatal("invalid nested transaction executed")
	}
	if got, err := CurrentTransaction(context.Background(), pool); got != nil || err != nil {
		t.Fatal("standalone transaction falsely bound")
	}
}
