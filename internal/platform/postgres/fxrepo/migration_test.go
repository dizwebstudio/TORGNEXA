package fxrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFXMigrationIsAppendOnly(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "migrations_legacy_pre_v1", "000040_fx_rate_provider_completion.sql")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, needle := range []string{"CREATE TABLE fx_rate_facts", "CREATE TABLE fx_resolution_evidence", "CREATE TABLE fx_conversion_records", "append-only", "REVOKE UPDATE, DELETE, TRUNCATE", "INSERT INTO migration_history"} {
		if !strings.Contains(s, needle) {
			t.Fatalf("migration missing %q", needle)
		}
	}
}
