package reconciliationrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMigrationIsTenantScopedDurableAndFailClosed(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "migrations_legacy_pre_v1", "000025_reconciliation.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, needle := range []string{
		"create table reconciliation_runs", "create table reconciliation_drifts", "create table reconciliation_actions",
		"force row level security", "reconciliation_runs_tenant_all", "reconciliation_drifts_tenant_all", "reconciliation_actions_tenant_all",
		"incremental','scheduled_full','on_demand", "content_drift','missing_mapping','orphan_mapping','duplicate_mapping','status_mismatch','stale_connector",
		"reconciliation drift evidence is immutable", "reconciliation action history is immutable", "completed reconciliation run is immutable", "insert into migration_history",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("migration missing %q", needle)
		}
	}
	for _, forbidden := range []string{"access_token", "authorization header", "raw_error", "payload_body"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("sensitive field %q found", forbidden)
		}
	}
}

func TestRepositoryUsesTenantPredicatesOptimisticVersionsAndAppendOnlyActions(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "repository.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, needle := range []string{"organization_id=$1 and workspace_id=$2", "version=version+1", "on conflict do nothing", "reconciliation_actions", "set_config('app.organization_id'", "set_config('app.workspace_id'"} {
		if !strings.Contains(text, needle) {
			t.Errorf("repository missing %q", needle)
		}
	}
}
