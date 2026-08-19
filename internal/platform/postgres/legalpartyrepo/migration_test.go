package legalpartyrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLegalPartyMigrationDefinesCanonicalMastersAndTenantGuards(t *testing.T) {
	_, f, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(f), "..", "..", "..", ".."))
	b, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000016_legal_party_counterparty.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	wants := []string{"create table legal_entities", "create table individual_entrepreneurs", "create table legal_branches", "create table counterparties", "create table counterparty_bank_accounts", "create table counterparty_contracts", "create table counterparty_authorities", "create table legal_party_duplicate_candidates", "create table legal_party_merge_previews", "force row level security", "legal-party master/history cannot be hard-deleted", "legal_party_ref_exists", "authority_reference"}
	for _, w := range wants {
		if !strings.Contains(s, w) {
			t.Fatalf("migration missing %q", w)
		}
	}
}
func TestLegalPartyMigrationKeepsProviderFieldsOutOfMasters(t *testing.T) {
	_, f, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(f), "..", "..", "..", ".."))
	b, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000016_legal_party_counterparty.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, bad := range []string{"ozon_", "wildberries_", "yandex_market_", "provider_counterparty_id"} {
		if strings.Contains(s, bad) {
			t.Fatalf("provider-specific token %q found", bad)
		}
	}
}
