package syncrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBootstrapScheduleMigrationIsTenantScopedAndBounded(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "migrations_legacy_pre_v1", "000062_connector_bootstrap_schedule.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, needle := range []string{
		"create table connector_bootstrap_previews", "create table connector_sync_schedules", "create table connector_sync_jobs",
		"force row level security", "security definer", "set row_security=off", "for update skip locked",
		"attempt_count<j.max_attempts", "bootstrap preview evidence is immutable", "insert into migration_history",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("migration missing %q", needle)
		}
	}
	for _, forbidden := range []string{"access_token", "authorization header", "credential_payload", "remote_payload"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("secret or payload-bearing token %q found", forbidden)
		}
	}
}
