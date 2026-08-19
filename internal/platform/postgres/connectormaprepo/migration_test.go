package connectormaprepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCatalogMigrationDefinesConnectorMappingBoundary(t *testing.T) {
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
		"CREATE TABLE CONNECTOR_ENTITY_MAPPINGS",
		"REFERENCES CONNECTOR_ACCOUNTS (ORGANIZATION_ID, WORKSPACE_ID, ID)",
		"ALTER TABLE CONNECTOR_ENTITY_MAPPINGS FORCE ROW LEVEL SECURITY",
		"CREATE POLICY CONNECTOR_ENTITY_MAPPINGS_TENANT_SELECT",
		"CREATE POLICY CONNECTOR_ENTITY_MAPPINGS_TENANT_INSERT",
		"CREATE POLICY CONNECTOR_ENTITY_MAPPINGS_TENANT_UPDATE",
		"REVOKE DELETE, TRUNCATE ON PRODUCTS, OFFERS, CONNECTOR_ENTITY_MAPPINGS FROM PUBLIC",
		"CONNECTOR_ENTITY_MAPPINGS_NO_DELETE",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("migration missing %q", required)
		}
	}
}
