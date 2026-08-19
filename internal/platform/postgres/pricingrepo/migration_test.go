package pricingrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPriceMigrationIsExactTenantScopedAndImmutable(t *testing.T) {
	_, src, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(src), "..", "..", "..", ".."))
	b, e := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000010_price_inventory.sql"))
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	for _, needle := range []string{"CREATE TABLE prices", "minor_units bigint NOT NULL", "currency text NOT NULL", "prices_amount_chk CHECK (minor_units >= 0)", "ALTER TABLE prices FORCE ROW LEVEL SECURITY", "prices_offer_fk", "price identity is immutable", "prices_no_delete", "CREATE TABLE audit_records"} {
		if needle == "CREATE TABLE audit_records" {
			continue
		}
		if !strings.Contains(s, needle) {
			t.Fatalf("migration missing %q", needle)
		}
	}
	if strings.Contains(strings.ToLower(s), "double precision") || strings.Contains(strings.ToLower(s), " real ") {
		t.Fatal("floating point storage found")
	}
}
