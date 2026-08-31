package logisticsrepo

import (
	"os"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/logistics"
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
	for _, state := range []string{"create_requested", "created", "cancel_requested", "cancelled", "cancel_unknown", "StatusUnknown"} {
		if !strings.Contains(source, state) {
			t.Fatalf("shipment lifecycle must include %q", state)
		}
	}
}

func TestCancelDigestKeepsLegacyDeliveryIdentity(t *testing.T) {
	id := logistics.ShipmentID("018f47a0-1234-7890-8abc-1234567890ab")
	legacy := cancelDigest(id, "3954004", "")
	if legacy != cancelDigest(id, "3954004", "delivery") {
		t.Fatal("delivery variant must preserve the legacy cancellation digest")
	}
	if legacy == cancelDigest(id, "3954004", "pickup") {
		t.Fatal("pickup variant must have a distinct cancellation digest")
	}
}
