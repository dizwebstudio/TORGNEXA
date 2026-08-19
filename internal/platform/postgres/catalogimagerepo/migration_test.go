package catalogimagerepo

import (
	"os"
	"strings"
	"testing"
)

func TestMigrationEnforcesTenantAndHTTPSImageReferences(t *testing.T) {
	b, err := os.ReadFile("../../../../migrations_legacy_pre_v1/000057_catalog_product_images.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, want := range []string{"create table catalog_product_images", "force row level security", "catalog_product_images_tenant_all", "^https://", "product image identity is immutable", "revoke delete, truncate"} {
		if !strings.Contains(s, want) {
			t.Errorf("migration missing %q", want)
		}
	}
}
