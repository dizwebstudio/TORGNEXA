package logistics

import (
	"strings"
	"testing"
	"time"
)

func TestCreateCommandRejectsUnboundedOrMissingIdempotency(t *testing.T) {
	command := CreateCommand{ID: "shipment-1", AccountID: "account-1", ExternalID: "order-1", ServiceCode: "cdek-136", PayloadReference: "sec:v1:0123456789abcdef0123456789abcdef", PayloadDigest: strings.Repeat("a", 64)}
	if command.Validate() == nil {
		t.Fatal("expected missing idempotency key to be rejected")
	}
	command.IdempotencyKey = "shipment-create-1"
	if err := command.Validate(); err != nil {
		t.Fatalf("valid create command rejected: %v", err)
	}
}

func TestRemoteResultRejectsNonCanonicalStatusAndTimezone(t *testing.T) {
	result := RemoteResult{RemoteID: "remote-1", Status: StatusDelivered, Currency: "RUB", ObservedAt: time.Now().UTC()}
	if err := result.Validate(); err != nil {
		t.Fatalf("valid remote result rejected: %v", err)
	}
	result.Status = Status("доставлен")
	if result.Validate() == nil {
		t.Fatal("expected provider text status to be rejected")
	}
	result.Status = StatusDelivered
	result.ObservedAt = time.Now()
	if result.Validate() == nil {
		t.Fatal("expected non-UTC observed time to be rejected")
	}
}

func TestShipmentKeepsRemoteIDOptionalBeforeRemoteCreate(t *testing.T) {
	shipment := Shipment{ID: "shipment-1", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", AccountID: "account-1", ExternalID: "order-1", ServiceCode: "cdek-136", Status: StatusPending, Currency: "RUB", Version: 1, UpdatedAt: time.Now().UTC()}
	if err := shipment.Validate(); err != nil {
		t.Fatalf("pending local shipment rejected: %v", err)
	}
}
