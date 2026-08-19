package auditrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAuditMigrationEnforcesRiskRLSAndAppendOnlyGuards(t *testing.T) {
	t.Parallel()
	migration := strings.ToUpper(strings.Join(strings.Fields(readAuditMigration(t)), " "))
	for _, fragment := range []string{
		"ADD COLUMN RISK TEXT NOT NULL DEFAULT",
		"AUDIT_RECORDS_RISK_CHK",
		"WRITE_SAFE",
		"WRITE_SENSITIVE",
		"LEGALLY_SIGNIFICANT",
		"AUDIT_RECORDS_SUMMARY_OBJECT_CHK",
		"AUDIT_RECORDS_SUMMARY_SIZE_CHK",
		"AUDIT_RECORDS_SUMMARY_REDACTION_CHK",
		"DROP POLICY AUDIT_RECORDS_TENANT_ISOLATION ON AUDIT_RECORDS",
		"CREATE POLICY AUDIT_RECORDS_TENANT_SELECT ON AUDIT_RECORDS FOR SELECT",
		"CREATE POLICY AUDIT_RECORDS_TENANT_INSERT ON AUDIT_RECORDS FOR INSERT",
		"CURRENT_SETTING('APP.ORGANIZATION_ID', TRUE)",
		"CURRENT_SETTING('APP.WORKSPACE_ID', TRUE)",
		"REVOKE UPDATE, DELETE, TRUNCATE ON AUDIT_RECORDS FROM PUBLIC",
		"CREATE TRIGGER AUDIT_RECORDS_NO_UPDATE_DELETE BEFORE UPDATE OR DELETE ON AUDIT_RECORDS",
		"CREATE TRIGGER AUDIT_RECORDS_NO_CLEAR BEFORE TRUNCATE ON AUDIT_RECORDS",
		"EXECUTE FUNCTION AUDIT_RECORDS_REJECT_MUTATION()",
		"INSERT INTO MIGRATION_HISTORY",
		"CURRENT_SETTING('TORGNEXA.MIGRATION_VERSION')",
	} {
		if !strings.Contains(migration, fragment) {
			t.Errorf("audit migration is missing %q", fragment)
		}
	}
	for _, forbiddenPolicy := range []string{
		"FOR UPDATE USING",
		"FOR DELETE USING",
		"FOR ALL USING",
	} {
		if strings.Contains(migration, forbiddenPolicy) {
			t.Errorf("audit migration contains mutation policy %q", forbiddenPolicy)
		}
	}
}

func TestAuditMigrationKeepsLegacyWriterCompatibility(t *testing.T) {
	t.Parallel()
	migration := strings.ToUpper(strings.Join(strings.Fields(readAuditMigration(t)), " "))
	if !strings.Contains(migration, "RISK TEXT NOT NULL DEFAULT 'UNCLASSIFIED'") {
		t.Fatal("risk column must keep a legacy-writer default during expand phase")
	}
	if strings.Contains(migration, "ALTER COLUMN ACTOR_ID SET NOT NULL") || strings.Contains(migration, "ALTER COLUMN CORRELATION_ID SET NOT NULL") {
		t.Fatal("expand migration must not break pre-Task-003 writers with stricter nullable columns")
	}
}

func readAuditMigration(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve audit migration test source")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", "..", "..", "migrations_legacy_pre_v1", "000004_audit_base.sql")
	data, err := os.ReadFile(path) // #nosec G304 -- path is derived from this compile-time test source path.
	if err != nil {
		t.Fatalf("read audit migration: %v", err)
	}
	return string(data)
}
