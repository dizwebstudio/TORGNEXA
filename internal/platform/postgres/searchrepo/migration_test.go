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

func TestProductSearchSQLProjectsDescriptionAndRegularPrice(t *testing.T) {
	for _, want := range []string{
		"p.description",
		"regular_price.minor_units",
		"regular_price.currency",
		"pr.kind='regular'",
		"e.status='active'",
	} {
		if !strings.Contains(productSearchSQL, want) {
			t.Fatalf("product search projection missing %q", want)
		}
	}
}

func TestLikePrefixEscapesSQLWildcards(t *testing.T) {
	if got, want := likePrefix(`50%_\sale`), `50\%\_\\sale%`; got != want {
		t.Fatalf("like prefix=%q want=%q", got, want)
	}
}

func TestDemoCatalogContainsTwentyFourVisualProducts(t *testing.T) {
	if got, want := len(demoCatalogProducts), 24; got != want {
		t.Fatalf("demo catalog size=%d want=%d", got, want)
	}
	seen := make(map[string]struct{}, len(demoCatalogProducts))
	for _, item := range demoCatalogProducts {
		if item.Code == "" || item.Title == "" || item.Description == "" || item.ImageAlt == "" {
			t.Fatalf("demo product %q is incomplete", item.Code)
		}
		if !strings.HasPrefix(item.ImageURL, "https://images.unsplash.com/") {
			t.Fatalf("demo product %q does not use an HTTPS Unsplash image", item.Code)
		}
		if _, exists := seen[item.Code]; exists {
			t.Fatalf("duplicate demo product code %q", item.Code)
		}
		seen[item.Code] = struct{}{}
	}
}

func TestDemoCatalogUsesDistinctInventorySKUs(t *testing.T) {
	seen := make(map[string]struct{}, len(demoCatalogProducts))
	for index := range demoCatalogProducts {
		sku := demoSKU(index)
		if sku == "" {
			t.Fatalf("demo product %d has an empty SKU", index)
		}
		if _, exists := seen[sku]; exists {
			t.Fatalf("duplicate demo SKU %q", sku)
		}
		seen[sku] = struct{}{}
	}
	if got, want := demoSKU(0), "DEMO-SKU"; got != want {
		t.Fatalf("primary demo SKU=%q want=%q", got, want)
	}
	if got, want := demoSKU(23), "DEMO-SKU-023"; got != want {
		t.Fatalf("last demo SKU=%q want=%q", got, want)
	}
}

func TestDemoStatusExamplesCoverVisibleLifecycleStatuses(t *testing.T) {
	catalogStatuses := make(map[string]bool)
	for _, item := range demoCatalogStatusProducts {
		if item.Code == "" || item.SKU == "" || item.Title == "" || item.Description == "" || item.ImageAlt == "" {
			t.Fatalf("demo status product %q is incomplete", item.Code)
		}
		if !strings.HasPrefix(item.ImageURL, "https://images.unsplash.com/") {
			t.Fatalf("demo status product %q does not use an HTTPS Unsplash image", item.Code)
		}
		catalogStatuses[item.Status] = true
	}
	for _, want := range []string{"draft", "archived"} {
		if !catalogStatuses[want] {
			t.Fatalf("demo catalog does not cover %s status", want)
		}
	}

	orderStatuses := map[string]bool{"pending": true}
	for _, target := range demoOrderStatusPaths {
		for _, status := range target.path {
			orderStatuses[status] = true
		}
	}
	for _, want := range []string{"pending", "confirmed", "processing", "fulfilled", "cancelled"} {
		if !orderStatuses[want] {
			t.Fatalf("demo orders do not cover %s status", want)
		}
	}

	incidentStatuses := map[string]bool{"needs_attention": true}
	for _, item := range demoIncidentHistory {
		incidentStatuses[item.status] = true
	}
	for _, want := range []string{"completed", "needs_attention", "resolved"} {
		if !incidentStatuses[want] {
			t.Fatalf("demo incidents do not cover %s status", want)
		}
	}
}
