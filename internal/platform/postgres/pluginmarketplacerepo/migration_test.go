package pluginmarketplacerepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMigrationEncodesImmutableTrustConsentRevocationAndTenantIsolation(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000028_plugin_marketplace_governance.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, needle := range []string{
		"create table plugin_marketplace_versions",
		"trust in ('official','verified','community')",
		"create table plugin_private_versions",
		"trust='private'",
		"create table plugin_marketplace_consents",
		"artifact_sha256",
		"create table plugin_marketplace_revocations",
		"kind in ('artifact','publisher_key')",
		"create table plugin_installation_revocations",
		"plugin marketplace governance evidence is append-only",
		"force row level security",
		"plugin_marketplace_consents_select",
		"plugin_installation_revocations_select",
		"insert into migration_history",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("migration missing %q", needle)
		}
	}
	for _, forbidden := range []string{"private_key", "access_token", "refresh_token", "credential_value", "secret_value"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("secret-bearing column %q found", forbidden)
		}
	}
}

func TestRepositoryUsesExactDigestConsentAndScopedPrivateEvidence(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "repository.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, needle := range []string{
		"set_config('app.organization_id'",
		"set_config('app.workspace_id'",
		"plugin_marketplace_versions",
		"plugin_private_versions",
		"plugin_marketplace_consents",
		"plugin_marketplace_revocations",
		"plugin_installation_revocations",
		"consent.grant.artifactsha256",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("repository missing %q", needle)
		}
	}
}
