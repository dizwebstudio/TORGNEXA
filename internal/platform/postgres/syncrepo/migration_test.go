package syncrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSyncMigrationIsTenantScopedDurableAndImmutableWhereRequired(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "migrations_legacy_pre_v1", "000024_sync_engine.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, needle := range []string{
		"create table sync_policies", "create table sync_checkpoints", "create table sync_entity_states",
		"create table sync_local_receipts", "create table sync_remote_receipts",
		"force row level security", "sync_policies_tenant_all", "sync_checkpoints_tenant_all", "sync_entity_states_tenant_all",
		"direction in ('inbound','outbound','bidirectional')", "source_of_truth in ('local','remote','manual')",
		"sync receipt history is immutable", "sync entity state progression is invalid", "insert into migration_history",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("migration missing %q", needle)
		}
	}
	for _, forbidden := range []string{"credential_payload", "access_token", "authorization header"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("provider/credential-specific token %q found", forbidden)
		}
	}
}

func TestRepositoryUsesExplicitTenantPredicatesAndOptimisticVersions(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "repository.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, needle := range []string{
		"organization_id=$1 and workspace_id=$2", "version=version+1", "on conflict do nothing",
		"sync_local_receipts", "sync_remote_receipts", "set_config('app.organization_id'", "set_config('app.workspace_id'",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("repository missing %q", needle)
		}
	}
}
