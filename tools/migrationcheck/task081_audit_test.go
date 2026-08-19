package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTask081CanonicalPartyAndRussianValidation(t *testing.T) {
	_, f, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(f), "../.."))
	checks := map[string][]string{
		"internal/core/legalparty/legalparty.go":                       {"validateinnlegal", "validateinnindividual", "validatekpp", "validateogrn", "validateogrnip", "detectduplicates", "buildmergepreview"},
		"internal/platform/postgres/legalpartyrepo/repository.go":      {"auditrepo.appendtransaction", "outboxrepo.newtransactionenqueuer", "lineagerepo.appendtransaction", "enterprise.legal_party.record_changed.v1"},
		"migrations_legacy_pre_v1/000016_legal_party_counterparty.sql": {"legal_entities", "individual_entrepreneurs", "counterparties", "counterparty_contracts", "counterparty_authorities", "force row level security"},
	}
	for rel, wants := range checks {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		s := strings.ToLower(string(b))
		for _, w := range wants {
			if !strings.Contains(s, w) {
				t.Fatalf("%s missing %q", rel, w)
			}
		}
	}
}
func TestTask081NoDownstreamProviderIdentityInCore(t *testing.T) {
	_, f, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(f), "../.."))
	b, err := os.ReadFile(filepath.Join(root, "internal/core/legalparty/legalparty.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, bad := range []string{"ozon_", "wildberries_", "wb_", "1c_id", "moysklad_id", "provider_id"} {
		if strings.Contains(s, bad) {
			t.Fatalf("legalparty core contains forbidden token %q", bad)
		}
	}
}
