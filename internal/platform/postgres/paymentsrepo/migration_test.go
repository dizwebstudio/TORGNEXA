package paymentsrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMigrationEncodesTenantScopeAmountAndLifecycleGuards(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, "migrations", "000018_payments_core.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, needle := range []string{
		"create table payments",
		"create table payment_refunds",
		"create table payment_webhook_receipts",
		"force row level security",
		"payments_amount_chk check(amount_minor_units>0)",
		"payments_remote_shape_chk",
		"payments_failure_shape_chk",
		"payments_succeeded_shape_chk",
		"payment_webhook_receipts_digest_chk",
		"revoke update,delete,truncate on payment_webhook_receipts from public",
		"insert into migration_history",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("migration missing %q", needle)
		}
	}
	for _, forbidden := range []string{"card_number", "pan text", "cvv", "cvc", " track2", "access_token", "refresh_token"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("card/secret field %q found in payments core migration", forbidden)
		}
	}
}

func TestRepositoryKeepsTenantScopeOutboxAuditAndSafeEventPayloads(t *testing.T) {
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
		"appendaudit(",
		"outboxrepo.newtransactionenqueuer",
		"commerce.payments.payment_status_changed.v1",
		"commerce.payments.refund_status_changed.v1",
		"risklegallysignificant",
		"version=$",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("repository missing %q", needle)
		}
	}
	for _, forbidden := range []string{"card_number", "\"pan\"", "cvv", "cvc"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("card field %q found in repository", forbidden)
		}
	}
}
