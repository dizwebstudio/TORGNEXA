package secretrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSecretsMigrationSecurityShape(t *testing.T) {
	t.Parallel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller unavailable")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", "..", "..", "migrations_legacy_pre_v1", "000005_secrets_provider.sql")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(data))
	required := []string{
		"create table secret_references", "create table secret_versions", "ciphertext bytea", "nonce bytea", "key_id text",
		"alter table secret_references force row level security", "alter table secret_versions force row level security",
		"secret_references_tenant_select", "secret_references_tenant_insert", "secret_references_tenant_update",
		"secret_versions_tenant_select", "secret_versions_tenant_insert", "secret_versions_reject_mutation",
		"aes-256-gcm", "connector_accounts_secret_reference_fk", "add column secret_reference text", "current_setting('app.organization_id', true)", "current_setting('app.workspace_id', true)",
	}
	for _, token := range required {
		if !strings.Contains(text, token) {
			t.Errorf("migration missing %q", token)
		}
	}

	definitionsEnd := strings.Index(text, "create index secret_references_tenant_status_idx")
	if definitionsEnd < 0 {
		t.Fatal("could not isolate table definitions")
	}
	definitions := text[:definitionsEnd]
	for _, forbidden := range []string{"\n  password ", "\n  token ", "\n  access_token ", "\n  refresh_token ", "\n  client_secret ", "\n  secret_value ", "\n  master_key "} {
		if strings.Contains(definitions, forbidden) {
			t.Errorf("plaintext/master-key column forbidden: %q", strings.TrimSpace(forbidden))
		}
	}
	if strings.Contains(text, "policy secret_versions_tenant_update") || strings.Contains(text, "policy secret_versions_tenant_delete") {
		t.Fatal("ciphertext versions unexpectedly have mutation RLS policies")
	}
}
