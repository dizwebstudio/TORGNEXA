package shopify

import "strings"

// shopifyVariant/shopifyProduct mirror Shopify Admin REST's inline variant
// shape (https://shopify.dev/docs/api/admin-rest/{version}/resources/product):
// unlike WooCommerce, variants travel inline on the product resource, there
// is no separate variations endpoint to page through.
type shopifyVariant struct {
	ID              int64  `json:"id"`
	SKU             string `json:"sku"`
	Price           string `json:"price"`
	CompareAtPrice  string `json:"compare_at_price"`
	InventoryItemID int64  `json:"inventory_item_id"`
	UpdatedAt       string `json:"updated_at"`
}
type shopifyProduct struct {
	ID        int64            `json:"id"`
	Title     string           `json:"title"`
	Status    string           `json:"status"`
	Vendor    string           `json:"vendor"`
	UpdatedAt string           `json:"updated_at"`
	Variants  []shopifyVariant `json:"variants"`
}
type shopifyLineItem struct {
	ID        int64 `json:"id"`
	ProductID int64 `json:"product_id"`
	VariantID int64 `json:"variant_id"`
	Quantity  int64 `json:"quantity"`
}
type shopifyOrder struct {
	ID                int64             `json:"id"`
	Name              string            `json:"name"`
	FinancialStatus   string            `json:"financial_status"`
	FulfillmentStatus string            `json:"fulfillment_status"`
	CancelledAt       *string           `json:"cancelled_at"`
	ClosedAt          *string           `json:"closed_at"`
	CreatedAt         string            `json:"created_at"`
	UpdatedAt         string            `json:"updated_at"`
	LineItems         []shopifyLineItem `json:"line_items"`
}
type shopifyRefundTransaction struct {
	Amount string `json:"amount"`
}
type shopifyRefund struct {
	ID           int64                      `json:"id"`
	CreatedAt    string                     `json:"created_at"`
	Note         string                     `json:"note"`
	Transactions []shopifyRefundTransaction `json:"transactions"`
}
type shopifyInventoryLevel struct {
	InventoryItemID int64 `json:"inventory_item_id"`
	LocationID      int64 `json:"location_id"`
	Available       int64 `json:"available"`
}
type shopifyLocation struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

func validRemoteText(value string, max int) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > max {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
