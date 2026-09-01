package customerservicerepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCustomerServiceMigrationKeepsHistoryTenantScopedAndRedacted(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "migrations", "000055_customer_service_inbox.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(data))
	for _, needle := range []string{
		"create table customer_service_customer_refs",
		"create table customer_service_conversations",
		"create table customer_service_messages",
		"create table customer_service_replies",
		"create table customer_service_assignments",
		"create table customer_service_sla_policies",
		"create table customer_service_attachments",
		"create table customer_service_findings",
		"force row level security",
		"customer_service_messages_no_mutation",
		"idempotency_key",
		"insert into migration_history",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("migration missing %q", needle)
		}
	}
	for _, forbidden := range []string{"access_token", "private key", "card_number", "raw_payload", "customer_email"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("sensitive field %q found in migration", forbidden)
		}
	}
}
