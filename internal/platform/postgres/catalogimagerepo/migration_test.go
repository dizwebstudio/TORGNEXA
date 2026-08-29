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

func TestDemoImagePathIsLimitedToBundledSVGAssets(t *testing.T) {
	for _, path := range []string{"/demo-images/demo-01.svg", "/demo-images/demo-26.svg"} {
		if !validImage(Image{URL: path}) {
			t.Fatalf("bundled demo image %q was rejected", path)
		}
	}
	for _, path := range []string{"/demo-images/demo-.svg", "/demo-images/demo-1.png", "/demo-images/../secret.svg"} {
		if validImage(Image{URL: path}) {
			t.Fatalf("invalid demo image path %q was accepted", path)
		}
	}
	if !validImage(Image{URL: "https://cdn.example.test/image.svg"}) {
		t.Fatal("external HTTPS image was rejected")
	}
}
