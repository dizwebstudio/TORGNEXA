package pimrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPIMMigrationDefinesTenantAndReviewGuards(t *testing.T) {
	_, f, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(f), "..", "..", "..", ".."))
	b, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000015_pim_mdm.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	wants := []string{"create table pim_brands", "create table pim_categories", "create table pim_attributes", "create table pim_field_authorities", "create table pim_duplicate_candidates", "create table pim_merge_previews", "force row level security", "duplicate candidate entities must exist in tenant", "merge preview entities must exist in tenant", "pim review evidence is append-only", "entity_type in ('product','offer','order','brand','category','attribute')"}
	for _, w := range wants {
		if !strings.Contains(s, w) {
			t.Fatalf("migration missing %q", w)
		}
	}
}
func TestPIMMigrationKeepsProviderFieldsOutOfMasters(t *testing.T) {
	_, f, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(f), "..", "..", "..", ".."))
	b, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000015_pim_mdm.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, bad := range []string{"ozon_", "wildberries_", "wb_", "yandex_market_", "marketplace_category_id"} {
		if strings.Contains(s, bad) {
			t.Fatalf("provider-specific token %q found", bad)
		}
	}
}
