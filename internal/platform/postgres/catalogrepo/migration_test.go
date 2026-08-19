package catalogrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCatalogMigrationDefinesTenantScopedLifecycle(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000009_catalog_domain.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(string(data))
	for _, required := range []string{
		"CREATE TABLE PRODUCTS", "CREATE TABLE OFFERS",
		"UNIQUE (ORGANIZATION_ID, WORKSPACE_ID, CODE)",
		"UNIQUE (ORGANIZATION_ID, WORKSPACE_ID, SKU)",
		"CATALOG_VALID_GTIN", "OFFERS_TENANT_GTIN_KEY",
		"ALTER TABLE PRODUCTS FORCE ROW LEVEL SECURITY", "ALTER TABLE OFFERS FORCE ROW LEVEL SECURITY",
		"CREATE POLICY PRODUCTS_TENANT_SELECT", "CREATE POLICY PRODUCTS_TENANT_INSERT", "CREATE POLICY PRODUCTS_TENANT_UPDATE",
		"CREATE POLICY OFFERS_TENANT_SELECT", "CREATE POLICY OFFERS_TENANT_INSERT", "CREATE POLICY OFFERS_TENANT_UPDATE",
		"PRODUCTS_GUARD_INSERT", "PRODUCTS_GUARD_UPDATE", "OFFERS_GUARD_INSERT", "OFFERS_GUARD_UPDATE",
		"NEW PRODUCT MUST START DRAFT AT VERSION 1", "NEW OFFER MUST START DRAFT AT VERSION 1",
		"PRODUCT HAS NON-ARCHIVED OFFERS", "ACTIVE OFFER REQUIRES ACTIVE PRODUCT",
		"INSERT INTO MIGRATION_HISTORY",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("migration missing %q", required)
		}
	}
}

func TestCatalogMigrationContainsNoProviderSpecificColumns(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	data, _ := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000009_catalog_domain.sql"))
	lower := strings.ToLower(string(data))
	for _, forbidden := range []string{"ozon_id", "wildberries_id", "wb_id", "marketplace_id"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("provider-specific core column %q", forbidden)
		}
	}
}
