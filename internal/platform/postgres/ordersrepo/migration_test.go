package ordersrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOrdersMigrationDefinesImmutableTenantScopedOrders(t *testing.T) {
	_, src, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(src), "..", "..", "..", ".."))
	b, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000011_orders.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToUpper(string(b))
	for _, want := range []string{"CREATE TABLE ORDERS", "CREATE TABLE ORDER_ITEMS", "ALTER TABLE ORDERS FORCE ROW LEVEL SECURITY", "ALTER TABLE ORDER_ITEMS FORCE ROW LEVEL SECURITY", "NEW ORDER MUST START PENDING AT VERSION 1", "ORDER COMMERCIAL SNAPSHOT IS IMMUTABLE", "ORDER ITEMS ARE IMMUTABLE", "ORDER TOTALS DO NOT MATCH IMMUTABLE ITEMS", "ENTITY_TYPE IN ('PRODUCT','OFFER','ORDER')", "INSERT INTO MIGRATION_HISTORY"} {
		if !strings.Contains(s, want) {
			t.Errorf("migration missing %q", want)
		}
	}
	for _, bad := range []string{"OZON_STATUS", "WB_STATUS", "WILDBERRIES_STATUS", "MARKETPLACE_STATUS", "FLOAT4", "FLOAT8", "DOUBLE PRECISION", " REAL "} {
		if strings.Contains(s, bad) {
			t.Fatalf("forbidden provider/float construct %q", bad)
		}
	}
}
