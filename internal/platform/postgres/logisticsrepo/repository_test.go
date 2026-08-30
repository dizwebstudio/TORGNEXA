package logisticsrepo

import (
	"os"
	"strings"
	"testing"
)

func TestRepositoryKeepsShipmentMutationAtomicAndCarrierNeutral(t *testing.T) {
	content, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	for _, fragment := range []string{
		"operation_receipts",
		"request_sha256",
		"auditrepo.AppendTransaction",
		"outboxrepo.NewTransactionEnqueuer",
		"logistics_shipments",
		"organization_id=$1 AND workspace_id=$2",
		"logistics_tracking_evidence",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("repository must contain %q", fragment)
		}
	}
	for _, forbidden := range []string{"http://", "https://", "net/http", "raw_payload", "authorization"} {
		if strings.Contains(strings.ToLower(source), forbidden) {
			t.Fatalf("repository must not contain carrier transport or secret payload %q", forbidden)
		}
	}
}

func TestRepositoryUsesExplicitNormalizedShipmentStates(t *testing.T) {
	content, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	for _, state := range []string{"create_requested", "created", "cancel_requested", "cancelled"} {
		if !strings.Contains(source, state) {
			t.Fatalf("shipment lifecycle must include %q", state)
		}
	}
}
