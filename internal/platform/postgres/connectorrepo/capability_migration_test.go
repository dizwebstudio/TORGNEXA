package connectorrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCapabilityMigrationIsTenantScopedAppendOnlyAndPolicyClassified(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000060_connector_account_capabilities.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sqlText := strings.ToUpper(string(data))
	for _, required := range []string{
		"CONNECTOR_ACCOUNT_CAPABILITY_HISTORY", "ACCOUNT_VERSION", "DIRECTION = 'READ'", "DIRECTION = 'WRITE'",
		"RISK_CLASS = 'WRITE_SENSITIVE'", "APPROVAL_REQUIRED = TRUE", "ENABLE ROW LEVEL SECURITY",
		"FORCE ROW LEVEL SECURITY", "CURRENT_SETTING('APP.ORGANIZATION_ID', TRUE)",
		"CURRENT_SETTING('APP.WORKSPACE_ID', TRUE)", "REVOKE UPDATE, DELETE, TRUNCATE",
		"CONNECTOR_ACCOUNT_CAPABILITY_HISTORY_NO_UPDATE", "CONNECTOR_ACCOUNT_CAPABILITY_HISTORY_NO_DELETE",
		"CONNECTOR_ACCOUNT_CAPABILITY_HISTORY_NO_CLEAR", "CAPABILITY HISTORY IS APPEND-ONLY",
	} {
		if !strings.Contains(sqlText, required) {
			t.Errorf("migration missing %q", required)
		}
	}
}
