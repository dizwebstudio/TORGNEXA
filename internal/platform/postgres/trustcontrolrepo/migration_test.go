package trustcontrolrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTrustControlMigrationIsTenantScopedAndAppendOnly(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve source")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", "..", "..", "migrations", "000016_trust_control_plane.sql")
	data, err := os.ReadFile(path) // #nosec G304 -- path is derived from the compile-time test source.
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(strings.Join(strings.Fields(string(data)), " "))
	for _, table := range []string{"OPERATION_RECEIPTS", "SECURITY_EVIDENCE", "AI_EGRESS_POLICY_REVISIONS", "AI_EGRESS_USAGE", "CONNECTOR_REPLAY_RUNS", "PROFITABILITY_SCENARIOS"} {
		for _, fragment := range []string{"CREATE TABLE " + table, "ALTER TABLE " + table + " ENABLE ROW LEVEL SECURITY", "ALTER TABLE " + table + " FORCE ROW LEVEL SECURITY"} {
			if !strings.Contains(sql, fragment) {
				t.Errorf("migration missing %q", fragment)
			}
		}
	}
	for _, fragment := range []string{"TRUST_APPEND_ONLY", "OPERATION_RECEIPTS_GUARD", "REQUEST_SHA256", "REVOKE UPDATE,DELETE,TRUNCATE ON SECURITY_EVIDENCE"} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration missing %q", fragment)
		}
	}
}
