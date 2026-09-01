package worker

import (
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/orders"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	materializationOrderID = "0199f7c4-0000-7000-8000-000000000001"
	materializationItemID  = "0199f7c4-0000-7000-8000-000000000002"
	materializationOfferID = "0199f7c4-0000-7000-8000-000000000003"
)

func TestBuildMarketplaceOrderCreateRequiresResolvedCanonicalEvidence(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	remote := sdk.RemoteOrder{RemoteID: "posting-1", ExternalID: "ORDER-1", StatusRemoteID: "awaiting_pack", CreatedAt: now, UpdatedAt: now, Items: []sdk.RemoteOrderItem{{RemoteID: "line-1", VariantRemoteID: "offer-remote-1", Quantity: 2}}}
	currency, _ := orders.NewCurrency("RUB")
	unit, _ := orders.NewUnitCode("PCS")
	decimal, _ := orders.NewDecimal(2, 0)
	quantity, _ := orders.NewQuantity(decimal, unit)
	price, _ := orders.NewMoney(1000, currency)
	subtotal, _ := orders.NewMoney(2000, currency)
	zero, _ := orders.NewMoney(0, currency)
	rate, _ := orders.NewDecimal(0, 0)
	line := orders.TaxSnapshot{Jurisdiction: "RU", Category: "zero", Rate: rate, PriceIncludesTax: false}
	command, err := BuildMarketplaceOrderCreate(remote, MarketplaceOrderMaterialization{OrderID: orders.OrderID(materializationOrderID), Number: "ORDER-1", Currency: currency, ShippingTotal: zero, PlacedAt: now, Lines: []MarketplaceOrderLine{{RemoteItemID: "line-1", OfferID: orders.OfferID(materializationOfferID), SKU: "SKU-1", Quantity: quantity, UnitPrice: price, Subtotal: subtotal, Discount: zero, TaxTotal: zero, LineTotal: subtotal, Tax: line, ItemID: orders.OrderItemID(materializationItemID)}}})
	if err != nil {
		t.Fatalf("complete remote snapshot rejected: %v", err)
	}
	if err := command.Validate(); err != nil || len(command.Items) != 1 {
		t.Fatalf("canonical create invalid: err=%v command=%#v", err, command)
	}
}

func TestBuildMarketplaceOrderCreateRejectsMissingMappingOrPricing(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	remote := sdk.RemoteOrder{RemoteID: "posting-1", ExternalID: "ORDER-1", StatusRemoteID: "awaiting_pack", CreatedAt: now, UpdatedAt: now, Items: []sdk.RemoteOrderItem{{RemoteID: "line-1", VariantRemoteID: "offer-remote-1", Quantity: 1}}}
	currency, _ := orders.NewCurrency("RUB")
	zero, _ := orders.NewMoney(0, currency)
	_, err := BuildMarketplaceOrderCreate(remote, MarketplaceOrderMaterialization{OrderID: orders.OrderID(materializationOrderID), Number: "ORDER-1", Currency: currency, ShippingTotal: zero, PlacedAt: now})
	if err == nil {
		t.Fatal("order without resolved mapping/pricing was materialized")
	}
}
