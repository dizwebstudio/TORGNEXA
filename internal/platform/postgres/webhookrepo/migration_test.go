package webhookrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDurableWebhookMigrationSecurityAndDurabilityInvariants(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "migrations_legacy_pre_v1", "000020_durable_webhooks.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	required := []string{
		"create table webhook_subscriptions", "create table webhook_deliveries", "create table webhook_delivery_attempts",
		"force row level security", "webhook_subscriptions_tenant_all", "webhook_deliveries_tenant_all",
		"webhook_delivery_attempts_tenant_select", "webhook_delivery_attempts_tenant_insert",
		"webhook_deliveries_initial_event_uniq", "webhook_deliveries_guard_update",
		"webhook_attempts_reject_mutation", "signing_secret_reference", "previous_signing_secret_reference", "previous_valid_until",
		"revoke update on webhook_delivery_attempts", "response bodies", "raw errors are forbidden",
	}
	for _, needle := range required {
		if !strings.Contains(text, needle) {
			t.Errorf("migration missing %q", needle)
		}
	}
	if strings.Contains(text, "plaintext_secret") || strings.Contains(text, "response_body text") || strings.Contains(text, "last_error text") {
		t.Fatal("migration introduced forbidden raw secret/response/error storage")
	}
}

func TestClaimQueryUsesSkipLockedAndTenantPredicates(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "repository.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, needle := range []string{"for update skip locked", "d.organization_id=$1", "d.workspace_id=$2", "status='inflight'", "lease_token", "on conflict (organization_id,workspace_id,subscription_id,event_id)", "set status='disabled'", "status='active'", "select * from changed"} {
		if !strings.Contains(text, needle) {
			t.Errorf("repository missing %q", needle)
		}
	}
}
