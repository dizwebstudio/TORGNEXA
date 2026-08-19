package inventoryrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInventoryMigrationUsesExactDecimalComponentsAndRLS(t *testing.T) {
	_, src, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(src), "..", "..", "..", ".."))
	b, e := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000010_price_inventory.sql"))
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	for _, n := range []string{"CREATE TABLE warehouses", "CREATE TABLE inventory_positions", "on_hand_coefficient bigint", "on_hand_scale smallint", "reserved_coefficient bigint", "reserved_scale smallint", "inventory_positions_reserved_lte_on_hand_chk", "ALTER TABLE inventory_positions FORCE ROW LEVEL SECURITY", "inventory position identity is immutable", "inactive parent permits reservation release only", "inventory_positions_no_delete"} {
		if !strings.Contains(s, n) {
			t.Fatalf("migration missing %q", n)
		}
	}
}

func TestWarehouseFailoverMigrationsArePersistentTenantScopedAndDoNotInventTransfers(t *testing.T) {
	_, src, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(src), "..", "..", "..", ".."))
	for _, tc := range []struct {
		file  string
		want  []string
		avoid []string
	}{
		{
			file: "000072_warehouse_operational_failover.sql",
			want: []string{"CREATE TABLE warehouse_operational_state", "CREATE TABLE warehouse_failover_routes", "CREATE TABLE warehouse_failover_decisions", "FORCE ROW LEVEL SECURITY", "unavailable", "lost"},
		},
		{
			file:  "000073_warehouse_incident_automation.sql",
			want:  []string{"CREATE TABLE warehouse_incidents", "CREATE TABLE warehouse_incident_decisions", "FORCE ROW LEVEL SECURITY", "warehouse_incident", "no_eligible_destination", "no row represents stock transfer"},
			avoid: []string{"UPDATE inventory_positions SET warehouse_id", "INSERT INTO inventory_positions SELECT"},
		},
		{
			file:  "000074_fulfillment_failover_execution.sql",
			want:  []string{"CREATE TABLE fulfillment_allocations", "fulfillment_allocations_one_reserved_item_idx", "FORCE ROW LEVEL SECURITY", "replaces_allocation_id", "untracked_reservation", "insufficient_capacity"},
			avoid: []string{"UPDATE inventory_positions SET warehouse_id", "UPDATE fulfillment_allocations SET warehouse_id", "INSERT INTO inventory_positions SELECT"},
		},
	} {
		body, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", tc.file))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		for _, want := range tc.want {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q", tc.file, want)
			}
		}
		for _, avoid := range tc.avoid {
			if strings.Contains(text, avoid) {
				t.Fatalf("%s contains unsafe stock-transfer pattern %q", tc.file, avoid)
			}
		}
	}
}
