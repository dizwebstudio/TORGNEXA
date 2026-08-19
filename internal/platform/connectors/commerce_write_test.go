package connectors

import (
	"testing"
	"time"
)

func TestCommerceWriteContractsValidate(t *testing.T) {
	if err := (ProductWriteRequest{SellerSKU: "SKU-1", Title: "Product", StatusRemoteID: "publish", IdempotencyKey: "pub-1"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (PriceWriteRequest{VariantRemoteID: "product:1", Value: "10.00", Currency: "USD", IdempotencyKey: "price-1"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (InventoryWriteRequest{VariantRemoteID: "product:1", Quantity: 2, IdempotencyKey: "stock-1"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (OrderStatusWriteRequest{OrderRemoteID: "41", StatusRemoteID: "processing", IdempotencyKey: "order-41"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (CommerceWriteReceipt{RemoteID: "41", Applied: true}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (CommerceWriteReceipt{RemoteID: "41", Applied: true, Duplicate: true}).Validate(); err == nil {
		t.Fatal("receipt accepted applied and duplicate simultaneously")
	}
}

func TestCommerceWebhookContractRequiresBoundTopicAndUTC(t *testing.T) {
	now := time.Date(2026, 8, 12, 8, 0, 0, 0, time.UTC)
	valid := CommerceWebhookRequest{Signature: "0123456789abcdef", HeaderTopic: "order.updated", ExpectedTopic: "order.updated", Body: []byte(`{"id":1}`), ReceivedAt: now}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	valid.ExpectedTopic = "product.updated"
	if err := valid.Validate(); err == nil {
		t.Fatal("mismatched expected topic accepted")
	}
}
