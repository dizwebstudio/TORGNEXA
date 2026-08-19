package connectorconfigrepo

import (
	"os"
	"strings"
	"testing"
)

func TestMigrationSecurityInvariants(t *testing.T) {
	raw, err := os.ReadFile("../../../../migrations_legacy_pre_v1/000068_connector_runtime_config.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, want := range []string{"create table connector_runtime_configs", "enable row level security", "force row level security", "connector_runtime_configs_nonsecret_chk", "insert into migration_history"} {
		if !strings.Contains(text, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
