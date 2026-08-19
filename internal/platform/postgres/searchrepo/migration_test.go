package searchrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))
}

func TestSearchMigrationDefinesPostgreSQLFTSWithoutSecondSourceOfTruth(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "migrations_legacy_pre_v1/000019_search_provider.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sqlText := strings.ToLower(string(data))
	for _, want := range []string{
		"search_product_vector", "search_offer_vector", "search_order_vector", "search_order_item_vector",
		"using gin(search_product_vector", "using gin(search_offer_vector", "using gin(search_order_vector", "using gin(search_order_item_vector",
		"products_tenant_code_lower_prefix_idx", "orders_tenant_number_lower_prefix_idx", "insert into migration_history",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("search migration missing %q", want)
		}
	}
	for _, bad := range []string{"create table search_", "opensearch", "elasticsearch", "tenant_id text"} {
		if strings.Contains(sqlText, bad) {
			t.Fatalf("search migration introduced forbidden independent/provider backend token %q", bad)
		}
	}
}

func TestSearchReliesOnForcedTenantRLSOfAuthoritativeTables(t *testing.T) {
	root := repoRoot(t)
	files := []string{"migrations_legacy_pre_v1/000009_catalog_domain.sql", "migrations_legacy_pre_v1/000011_orders.sql"}
	combined := ""
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		combined += strings.ToLower(string(data))
	}
	for _, table := range []string{"products", "offers", "orders", "order_items"} {
		if !strings.Contains(combined, "alter table "+table+" force row level security") {
			t.Fatalf("%s is not protected by forced RLS", table)
		}
	}
}

func TestSearchSQLCarriesTenantPredicatesIntoRootAndChildMatches(t *testing.T) {
	if !strings.Contains(applyScope, "app.organization_id") || !strings.Contains(applyScope, "app.workspace_id") {
		t.Fatal("transaction-local tenant scope is incomplete")
	}
	for name, query := range map[string]string{"products": productSearchSQL, "orders": orderSearchSQL} {
		if strings.Count(query, "organization_id=$1") < 3 || strings.Count(query, "workspace_id=$2") < 3 {
			t.Fatalf("%s search does not repeat tenant predicates across root/child match paths", name)
		}
		if !strings.Contains(query, "ORDER BY priority ASC,updated_at DESC,id DESC") {
			t.Fatalf("%s search lacks deterministic keyset order", name)
		}
	}
}

func TestLikePrefixEscapesSQLWildcards(t *testing.T) {
	if got, want := likePrefix(`50%_\sale`), `50\%\_\\sale%`; got != want {
		t.Fatalf("like prefix=%q want=%q", got, want)
	}
}
