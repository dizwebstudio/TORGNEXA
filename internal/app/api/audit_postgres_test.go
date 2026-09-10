package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"os"
	"testing"
	"time"
)

// auditPostgres opens only an explicitly supplied disposable integration database.
// Fixture setup uses a separate administrator; application calls use forced RLS.
func auditPostgres(t *testing.T) (context.Context, *sql.DB, *sql.DB, tenancy.Scope) {
	t.Helper()
	dsn, adminDSN := os.Getenv("TORGNEXA_TEST_DATABASE_URL"), os.Getenv("TORGNEXA_TEST_ADMIN_DATABASE_URL")
	if dsn == "" || adminDSN == "" {
		t.Skip("requires disposable PostgreSQL; see scripts/check-audit-postgres.sh")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	var privileged bool
	if err := db.QueryRowContext(ctx, `SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&privileged); err != nil || privileged {
		t.Fatal("integration application role must be unprivileged", err)
	}
	scope, err := tenancy.ParseScope(auditFixtureID(), auditFixtureID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO organizations(id,name) VALUES($1,'Synthetic audit')`, scope.OrganizationID().String()); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO workspaces(id,organization_id,name) VALUES($1,$2,'Synthetic workspace')`, scope.WorkspaceID().String(), scope.OrganizationID().String()); err != nil {
		t.Fatal(err)
	}
	return ctx, db, admin, scope
}
func auditFixtureID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	id[6] = (id[6] & 15) | 0x70
	id[8] = (id[8] & 63) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}
