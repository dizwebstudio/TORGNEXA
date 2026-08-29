package architecture

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCanonicalProviderPathsAcceptCategorizedAndLegacyForms(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		id   string
		want bool
	}{
		{path: "connectors/marketplaces/ozon", id: "ozon", want: true},
		{path: "connectors/ozon", id: "ozon", want: true},
		{path: "plugins/local/ollama", id: "ollama", want: true},
		{path: "connectors/marketplaces/other", id: "ozon", want: false},
		{path: "connectors/invalid_category/ozon", id: "ozon", want: false},
		{path: "connectors/marketplaces/ozon/internal", id: "ozon", want: false},
	}
	for _, test := range tests {
		if got := canonicalProviderImplementation(test.path, test.id); got != test.want {
			t.Errorf("canonicalProviderImplementation(%q, %q) = %v, want %v", test.path, test.id, got, test.want)
		}
	}
	if !canonicalProviderManifest("connectors/marketplaces/ozon/manifest.json", "ozon") {
		t.Fatal("categorized manifest should be canonical")
	}
	if canonicalProviderManifest("docs/connectors/ozon/manifest.json", "ozon") {
		t.Fatal("documentation path must not be a provider manifest")
	}
	if !providerImplementationMatchesFamily("connectors/marketplaces/ozon", "marketplace") {
		t.Fatal("marketplace provider should use the marketplaces category")
	}
	if providerImplementationMatchesFamily("connectors/storefronts/ozon", "marketplace") {
		t.Fatal("marketplace provider must not use the storefronts category")
	}
}

func TestDiscoverProviderImplementationsSupportsOneCategoryLevel(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTestFile(t, root, "connectors/legacy/manifest.json", "{}\n")
	writeTestFile(t, root, "connectors/marketplaces/ozon/manifest.json", "{}\n")
	found := &problems{}
	got := (&repository{root: root}).discoverProviderImplementations(context.Background(), "connectors", found)
	want := []discoveredProvider{
		{ID: "legacy", Path: filepath.ToSlash(filepath.Join("connectors", "legacy"))},
		{ID: "ozon", Path: filepath.ToSlash(filepath.Join("connectors", "marketplaces", "ozon"))},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverProviderImplementations() = %#v, want %#v", got, want)
	}
	if err := found.err(); err != nil {
		t.Fatalf("discoverProviderImplementations() diagnostics = %v", err)
	}
}
