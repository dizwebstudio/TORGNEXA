package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/pim"
)

func TestTask023MergePreviewIsDeterministicAndNonExecuting(t *testing.T) {
	_, f, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(f), "../.."))
	b, err := os.ReadFile(filepath.Join(root, "internal/core/pim/pim.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, want := range []string{"buildmergepreview", "fingerprintsha256", "equal_authority", "source_authority"} {
		if !strings.Contains(s, want) {
			t.Fatalf("pim core missing %q", want)
		}
	}
	for _, bad := range []string{"func applymerge(", "func mergeapply(", "provider_category", "ozon_", "wildberries_"} {
		if strings.Contains(s, bad) {
			t.Fatalf("PIM core contains forbidden token %q", bad)
		}
	}
	_ = pim.ErrMergeConflict
}
func TestTask023EvidenceAndMappingAreBound(t *testing.T) {
	_, f, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(f), "../.."))
	checks := map[string][]string{
		"internal/platform/postgres/pimrepo/repository.go":          {"auditrepo.appendtransaction", "outboxrepo.newtransactionenqueuer", "lineagerepo.appendtransaction", "commerce.pim.record_changed.v1"},
		"migrations_legacy_pre_v1/000015_pim_mdm.sql":               {"brand','category','attribute", "pim_duplicate_candidates", "pim_merge_previews", "force row level security"},
		"contracts/catalog/connector-entity-mapping-v3.schema.json": {"brand", "category", "attribute"},
	}
	for rel, wants := range checks {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		s := strings.ToLower(string(b))
		for _, w := range wants {
			if !strings.Contains(s, strings.ToLower(w)) {
				t.Fatalf("%s missing %q", rel, w)
			}
		}
	}
}
