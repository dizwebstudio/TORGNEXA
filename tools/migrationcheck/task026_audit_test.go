package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTask026SearchProviderIsTenantScopedAndProviderNeutral(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	checks := map[string][]string{
		"internal/platform/search/search.go": {
			"type Provider interface", "SearchProducts(context.Context, tenancy.Scope", "SearchOrders(context.Context, tenancy.Scope", "ProductFingerprint", "OrderFingerprint",
		},
		"internal/platform/postgres/searchrepo/repository.go": {
			"set_config('app.organization_id'", "set_config('app.workspace_id'", "organization_id=$1", "workspace_id=$2", "websearch_to_tsquery", "ORDER BY priority ASC,updated_at DESC,id DESC",
		},
		"migrations_legacy_pre_v1/000019_search_provider.sql": {
			"search_product_vector", "search_offer_vector", "search_order_vector", "search_order_item_vector", "USING GIN", "INSERT INTO migration_history",
		},
	}
	for rel, wants := range checks {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q", rel, want)
			}
		}
		lower := strings.ToLower(text)
		for _, bad := range []string{"opensearch", "elasticsearch"} {
			if strings.Contains(lower, bad) {
				t.Fatalf("%s contains premature external backend token %q", rel, bad)
			}
		}
	}
}

func TestTask026APIHasNoClientControlledTenantScope(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	data, err := os.ReadFile(filepath.Join(root, "internal/app/api/search.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "resolver.SearchScope(r)") {
		t.Fatal("search API does not use authenticated scope resolver")
	}
	for _, bad := range []string{`Get("organization_id")`, `Get("workspace_id")`, `Header.Get("X-Organization`, `Header.Get("X-Workspace`} {
		if strings.Contains(text, bad) {
			t.Fatalf("search API trusts client tenant selector %q", bad)
		}
	}
}
