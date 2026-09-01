package worker

import (
	"errors"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/orders"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var ErrMarketplaceOrderMaterialization = errors.New("worker: marketplace order materialization requires canonical evidence")

// MarketplaceOrderLine is the host-side evidence needed to turn a remote
// order line into the canonical orders aggregate. Remote IDs are lookup keys;
// OfferID and pricing must already be resolved by the tenant-scoped host.
type MarketplaceOrderLine struct {
	RemoteItemID string
	OfferID      orders.OfferID
	SKU          string
	Quantity     orders.Quantity
	UnitPrice    orders.Money
	Subtotal     orders.Money
	Discount     orders.Money
	TaxTotal     orders.Money
	LineTotal    orders.Money
	Tax          orders.TaxSnapshot
	ItemID       orders.OrderItemID
}

// MarketplaceOrderMaterialization is the explicit, provider-neutral input to
// canonical order creation. The repository call remains separate so the
// caller can put mapping, order insert, audit and outbox in its transaction.
type MarketplaceOrderMaterialization struct {
	OrderID       orders.OrderID
	Number        string
	Currency      orders.Currency
	ShippingTotal orders.Money
	PlacedAt      time.Time
	Lines         []MarketplaceOrderLine
}

// BuildMarketplaceOrderCreate verifies that a remote snapshot is complete
// enough for canonical materialization. It never guesses an offer, price,
// tax, quantity or status, and it never stores the remote provider payload.
func BuildMarketplaceOrderCreate(remote sdk.RemoteOrder, input MarketplaceOrderMaterialization) (orders.CreateOrder, error) {
	if remote.Validate() != nil || !input.OrderID.Valid() || input.Currency.Validate() != nil || input.ShippingTotal.Validate() != nil || input.ShippingTotal.Currency() != input.Currency || input.PlacedAt.IsZero() || input.PlacedAt.Location() != time.UTC || len(input.Lines) != len(remote.Items) || len(input.Lines) == 0 {
		return orders.CreateOrder{}, ErrMarketplaceOrderMaterialization
	}
	byRemoteItem := make(map[string]MarketplaceOrderLine, len(input.Lines))
	for _, line := range input.Lines {
		if line.RemoteItemID == "" || !line.OfferID.Valid() || !line.ItemID.Valid() || line.Quantity.Validate() != nil || line.UnitPrice.Validate() != nil || line.Subtotal.Validate() != nil || line.Discount.Validate() != nil || line.TaxTotal.Validate() != nil || line.LineTotal.Validate() != nil || line.Tax.Validate() != nil {
			return orders.CreateOrder{}, ErrMarketplaceOrderMaterialization
		}
		if line.SKU == "" || line.UnitPrice.Currency() != input.Currency || line.Subtotal.Currency() != input.Currency || line.Discount.Currency() != input.Currency || line.TaxTotal.Currency() != input.Currency || line.LineTotal.Currency() != input.Currency || line.Discount.MinorUnits() > line.Subtotal.MinorUnits() {
			return orders.CreateOrder{}, ErrMarketplaceOrderMaterialization
		}
		if _, exists := byRemoteItem[line.RemoteItemID]; exists {
			return orders.CreateOrder{}, ErrMarketplaceOrderMaterialization
		}
		byRemoteItem[line.RemoteItemID] = line
	}
	items := make([]orders.CreateItem, 0, len(remote.Items))
	for index, remoteItem := range remote.Items {
		line, ok := byRemoteItem[remoteItem.RemoteID]
		if !ok || line.Quantity.Value.Coefficient() != remoteItem.Quantity || line.Quantity.Value.Scale() != 0 {
			return orders.CreateOrder{}, ErrMarketplaceOrderMaterialization
		}
		if line.SKU == "" {
			return orders.CreateOrder{}, ErrMarketplaceOrderMaterialization
		}
		items = append(items, orders.CreateItem{ID: line.ItemID, OfferID: line.OfferID, Position: index + 1, SKU: line.SKU, Quantity: line.Quantity, UnitPrice: line.UnitPrice, Subtotal: line.Subtotal, DiscountTotal: line.Discount, TaxTotal: line.TaxTotal, LineTotal: line.LineTotal, Tax: line.Tax})
	}
	command := orders.CreateOrder{ID: input.OrderID, Number: input.Number, Currency: input.Currency, Items: items, ShippingTotal: input.ShippingTotal, PlacedAt: input.PlacedAt}
	if command.Validate() != nil {
		return orders.CreateOrder{}, ErrMarketplaceOrderMaterialization
	}
	return command, nil
}
