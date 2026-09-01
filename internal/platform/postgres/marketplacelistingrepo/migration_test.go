package marketplacelistingrepo

import (
	"os"
	"strings"
	"testing"
)

func TestListingWorkspaceMigrationKeepsEvidenceTenantScopedAndAppendOnly(t *testing.T) {
	data, err := os.ReadFile("../../../../migrations/000052_marketplace_listing_workspace.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(data))
	for _, want := range []string{
		"create table marketplace_listing_taxonomies",
		"create table marketplace_listing_batches",
		"force row level security",
		"marketplace_listing_taxonomies_tenant_all",
		"marketplace_listing_batches_tenant_all",
		"marketplace_listing_taxonomies_no_update_delete",
		"marketplace_listing_batches_no_update_delete",
		"marketplace_listing_batch_idempotency_uq",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("migration missing %q", want)
		}
	}
	for _, forbidden := range []string{"authorization", "access_token", "client_secret", "raw_payload"} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("migration contains forbidden sensitive marker %q", forbidden)
		}
	}
}
