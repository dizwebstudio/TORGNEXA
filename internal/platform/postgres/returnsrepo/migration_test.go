package returnsrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReturnsMigrationKeepsFactsTenantScopedAndAppendOnly(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, "migrations", "000029_returns_cancellations_refunds.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, needle := range []string{
		"create table order_cancellations",
		"create table commerce_returns",
		"create table return_items",
		"create table refund_allocations",
		"create table commerce_operation_evidence",
		"force row level security",
		"idempotency_key",
		"insert into migration_history",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("migration missing %q", needle)
		}
	}
	for _, forbidden := range []string{"card_number", "pan text", "cvv", "access_token", "private key"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("secret/payment field %q found in migration", forbidden)
		}
	}
}
