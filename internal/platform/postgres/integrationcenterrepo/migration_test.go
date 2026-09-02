package integrationcenterrepo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegrationCenterMigrationHasTenantSafetyAndImmutableEvidence(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "../../../../migrations/000035_integration_state_center.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(data))
	for _, required := range []string{"create table integration_center_snapshots", "create table integration_center_snapshot_accounts", "create table integration_center_status_transitions", "create table integration_center_action_receipts", "create table integration_center_recompute_queue", "force row level security", "integration_center_immutable", "integration_center_queue_claim_idx", "revoke update, delete, truncate"} {
		if !strings.Contains(sql, required) {
			t.Errorf("migration missing %q", required)
		}
	}
}
