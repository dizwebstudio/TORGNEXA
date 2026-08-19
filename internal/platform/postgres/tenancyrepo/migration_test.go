package tenancyrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTenancyMigrationStaticSafety(t *testing.T) {
	t.Parallel()
	migration := readTenancyMigration(t)
	normalized := strings.ToUpper(strings.Join(strings.Fields(migration), " "))
	if !strings.HasPrefix(normalized, "BEGIN;") || !strings.HasSuffix(normalized, "COMMIT;") {
		t.Fatal("tenancy migration must be one explicit transaction")
	}
	for _, forbidden := range []string{"DROP TABLE", "TRUNCATE ", "DELETE FROM", " ON DELETE CASCADE"} {
		if strings.Contains(normalized, forbidden) {
			t.Errorf("tenancy migration contains destructive operation %q", forbidden)
		}
	}
	for _, guard := range []string{"SET LOCAL LOCK_TIMEOUT", "SET LOCAL STATEMENT_TIMEOUT"} {
		if !strings.Contains(normalized, guard) {
			t.Errorf("tenancy migration is missing %q", guard)
		}
	}
	for _, fragment := range []string{
		"CREATE TABLE STORES",
		"FOREIGN KEY (ORGANIZATION_ID, WORKSPACE_ID) REFERENCES WORKSPACES (ORGANIZATION_ID, ID)",
		"UNIQUE (ORGANIZATION_ID, WORKSPACE_ID, ID)",
		"UNIQUE (ORGANIZATION_ID, WORKSPACE_ID, CODE)",
		"STORES_WORKSPACE_STATUS_IDX",
		"OR ID ~ '^[0-7][0-9A-HJKMNP-TV-Z]{25}$'",
	} {
		if !strings.Contains(normalized, fragment) {
			t.Errorf("tenancy migration is missing invariant %q", fragment)
		}
	}
	for _, constraint := range []string{
		"CONNECTOR_ACCOUNTS_WORKSPACE_SCOPE_FK",
		"OUTBOX_EVENTS_WORKSPACE_SCOPE_FK",
		"AUDIT_RECORDS_WORKSPACE_SCOPE_FK",
	} {
		if !strings.Contains(normalized, constraint) || !strings.Contains(normalized, constraint+" FOREIGN KEY") {
			t.Errorf("tenancy migration is missing scoped constraint %q", constraint)
		}
		if !strings.Contains(normalized, "VALIDATE CONSTRAINT "+constraint) {
			t.Errorf("tenancy migration leaves constraint %q unvalidated", constraint)
		}
	}
}

func TestTenancyMigrationForcesDefaultDenyRLS(t *testing.T) {
	t.Parallel()
	migration := strings.ToLower(strings.Join(strings.Fields(readTenancyMigration(t)), " "))
	for _, table := range []string{"organizations", "workspaces", "stores", "connector_accounts", "outbox_events", "audit_records"} {
		for _, fragment := range []string{
			"alter table " + table + " enable row level security",
			"alter table " + table + " force row level security",
			"create policy " + table + "_tenant_isolation on " + table,
		} {
			if !strings.Contains(migration, fragment) {
				t.Errorf("tenancy migration is missing %q", fragment)
			}
		}
	}
	for _, setting := range []string{
		"current_setting('app.organization_id', true)",
		"current_setting('app.workspace_id', true)",
	} {
		if !strings.Contains(migration, setting) {
			t.Errorf("tenancy migration is missing default-deny setting %q", setting)
		}
	}
	for _, fragment := range []string{
		"alter table inbox_events enable row level security",
		"alter table inbox_events force row level security",
	} {
		if !strings.Contains(migration, fragment) {
			t.Errorf("reserved inbox table is missing deny-all guard %q", fragment)
		}
	}
	if strings.Contains(migration, "create policy inbox_events") {
		t.Fatal("reserved inbox table must remain deny-all until Task 009")
	}
}

func readTenancyMigration(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve migration test source")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", "..", "..", "migrations_legacy_pre_v1", "000002_tenancy.sql")
	// #nosec G304 -- the path is derived only from this compile-time test source path.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tenancy migration: %v", err)
	}
	return string(data)
}
