package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationFrameworkSQLKeepsFencingAndMetadataInvariants(t *testing.T) {
	t.Parallel()
	path := filepath.Join(testRepositoryRoot(t), "migrations", "000003_migration_framework.sql")
	data, err := os.ReadFile(path) // #nosec G304 -- compile-time repository path.
	if err != nil {
		t.Fatalf("read migration framework SQL: %v", err)
	}
	normalized := strings.ToUpper(strings.Join(strings.Fields(string(data)), " "))
	for _, fragment := range []string{
		"BEGIN;",
		"SET LOCAL LOCK_TIMEOUT",
		"SET LOCAL STATEMENT_TIMEOUT",
		"CREATE TABLE MIGRATION_HISTORY",
		"CHECKSUM_SHA256",
		"CREATE TABLE BACKFILL_JOBS",
		"LEASE_GENERATION",
		"UNIQUE NULLS NOT DISTINCT (JOB_KEY, ORGANIZATION_ID, WORKSPACE_ID)",
		"FOREIGN KEY (ORGANIZATION_ID, WORKSPACE_ID) REFERENCES WORKSPACES (ORGANIZATION_ID, ID) ON DELETE RESTRICT",
		"REVOKE ALL ON MIGRATION_HISTORY FROM PUBLIC",
		"REVOKE ALL ON BACKFILL_JOBS FROM PUBLIC",
		"COMMIT;",
	} {
		if !strings.Contains(normalized, fragment) {
			t.Errorf("migration framework is missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"DROP TABLE", "DROP COLUMN", "TRUNCATE ", "DELETE FROM", "ON DELETE CASCADE"} {
		if strings.Contains(normalized, forbidden) {
			t.Errorf("migration framework contains destructive construct %q", forbidden)
		}
	}
}

func TestAtomicMigrationTemplateRecordsHistoryInsideTransaction(t *testing.T) {
	t.Parallel()
	path := filepath.Join(testRepositoryRoot(t), "templates", "migration.sql.tmpl")
	data, err := os.ReadFile(path) // #nosec G304 -- compile-time repository path.
	if err != nil {
		t.Fatalf("read migration SQL template: %v", err)
	}
	migration := Migration{Version: 4, Phase: PhaseExpand, Policy: "v1", HistoryMode: "atomic"}
	if err := validateSQL(migration, data); err != nil {
		t.Fatalf("validateSQL(template) error = %v", err)
	}
	withoutSettings := []byte(strings.ReplaceAll(string(data), "current_setting", "catalog_value"))
	if err := validateSQL(migration, withoutSettings); err == nil || !strings.Contains(err.Error(), "atomic history mode") {
		t.Fatalf("validateSQL(without settings) error = %v", err)
	}
}
