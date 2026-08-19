package connectors

import (
	"testing"
	"time"
)

func TestCommercePriceOrderNotificationBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	price := RemotePrice{VariantRemoteID: "SKU-1", Value: "1999.90", CompareAt: "2499", Currency: "RUR", VATRemoteID: "14", UpdatedAt: now}
	if err := price.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"1e3", "01", "-1", ".5", "1."} {
		price.Value = bad
		if price.Validate() == nil {
			t.Fatalf("bad price accepted %q", bad)
		}
	}
	order := RemoteOrder{RemoteID: "1001", CampaignRemoteID: "2001", ProgramRemoteID: "FBS", StatusRemoteID: "PROCESSING", CreatedAt: now, UpdatedAt: now.Add(time.Minute), Items: []RemoteOrderItem{{RemoteID: "1", VariantRemoteID: "SKU-1", Quantity: 2}}}
	if err := order.Validate(); err != nil {
		t.Fatal(err)
	}
	order.UpdatedAt = now.Add(-time.Second)
	if order.Validate() == nil {
		t.Fatal("order time reversal accepted")
	}
	n := MarketplaceNotification{Type: "ORDER_UPDATED", CampaignRemoteID: "2001", ResourceKind: "order", ResourceRemoteID: "1001", OccurredAt: now, DedupKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	if err := n.Validate(); err != nil {
		t.Fatal(err)
	}
}
