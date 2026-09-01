package connectors

import (
	"testing"
	"time"
)

func TestMarketplaceOperationRequestsAreTypedAndExact(t *testing.T) {
	reservation := ReservationRequest{OrderRemoteID: "order-1", VariantRemoteID: "sku-1", Quantity: "1.25", Unit: "PCS", IdempotencyKey: "reserve-1"}
	if err := reservation.Validate(); err != nil {
		t.Fatalf("reservation rejected: %v", err)
	}
	reservation.Quantity = "1.2500000000"
	if err := reservation.Validate(); err == nil {
		t.Fatal("quantity above fixed-point scale accepted")
	}
	fulfillment := MarketplaceFulfillmentRequest{OrderRemoteID: "order-1", Action: FulfillmentCreateShipment, IdempotencyKey: "shipment-1"}
	if err := fulfillment.Validate(); err != nil {
		t.Fatalf("fulfillment rejected: %v", err)
	}
}

func TestMarketplaceOperationReceiptRequiresRemoteEvidenceForApplied(t *testing.T) {
	receipt := MarketplaceOperationReceipt{Status: MarketplaceOperationApplied, ObservedAt: time.Now().UTC()}
	if err := receipt.Validate(); err == nil {
		t.Fatal("applied receipt without remote identity accepted")
	}
	receipt.RemoteOperationID = "op-1"
	if err := receipt.Validate(); err != nil {
		t.Fatalf("valid applied receipt rejected: %v", err)
	}
}
