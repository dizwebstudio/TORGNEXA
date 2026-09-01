package catalogbulkrepo

import (
	"os"
	"strings"
	"testing"
)

func TestMassCatalogMigrationIsTenantScopedAppendOnlyAndRedacted(t *testing.T) {
	data, err := os.ReadFile("../../../../migrations/000057_mass_catalog_management.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(data))
	for _, want := range []string{
		"create table catalog_bulk_previews",
		"create table catalog_bulk_runs",
		"create table catalog_bulk_kill_switches",
		"force row level security",
		"catalog_bulk_previews_no_update_delete",
		"catalog_bulk_runs_no_update_delete",
		"catalog_bulk_kill_switches_no_update_delete",
		"catalog_bulk_run_idempotency_uq",
		"catalog_bulk_run_preview_fk",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("migration missing %q", want)
		}
	}
	if strings.Count(sql, "force row level security") != 3 {
		t.Fatalf("every evidence table must use FORCE RLS")
	}
	if !strings.Contains(sql, "preview_document::text !~*") || !strings.Contains(sql, "run_document::text !~*") {
		t.Fatal("JSON evidence guards must reject secret-shaped keys and scripts")
	}
}
