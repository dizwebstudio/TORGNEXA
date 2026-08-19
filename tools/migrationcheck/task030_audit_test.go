package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/platform/lineage"
)

func TestTask030DeterministicLineageIDs(t *testing.T) {
	a, err := lineage.DeterministicID("evt.price.42")
	if err != nil {
		t.Fatal(err)
	}
	b, err := lineage.DeterministicID("evt.price.42")
	if err != nil {
		t.Fatal(err)
	}
	if a != b || !strings.HasPrefix(a, "lin.") {
		t.Fatalf("unexpected lineage ids %q %q", a, b)
	}
}

func TestTask030MigrationAndDomainAdaptersAreLineageBound(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	checks := map[string][]string{
		"migrations_legacy_pre_v1/000014_data_lineage.sql":       {"force row level security", "lineage audit evidence must belong to same tenant", "lineage event evidence must belong to same tenant", "lineage evidence is append-only"},
		"internal/platform/postgres/pricingrepo/repository.go":   {"appendpricelineage", "lineagerepo.appendtransaction", "field: \"amount\""},
		"internal/platform/postgres/inventoryrepo/repository.go": {"appendpositionlineage", "lineagerepo.appendtransaction", "field: \"stock\""},
	}
	for rel, wants := range checks {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		s := strings.ToLower(string(b))
		for _, want := range wants {
			if !strings.Contains(s, strings.ToLower(want)) {
				t.Fatalf("%s missing %q", rel, want)
			}
		}
	}
}

func TestTask030LineageDoesNotCopyBusinessPayloads(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	b, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1/000014_data_lineage.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, bad := range []string{"payload jsonb", "before_payload", "after_payload", "access_token", "refresh_token", "client_secret", "password"} {
		if strings.Contains(s, bad) {
			t.Fatalf("lineage migration contains forbidden payload/secret token %q", bad)
		}
	}
}
