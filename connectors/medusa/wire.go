package medusa

import (
	"encoding/json"
	"strings"
)

// medusaPrice.Amount uses json.Number rather than float64: Medusa v2 stores
// price amounts as decimal major-unit numbers (20 or 20.5, not cents), and
// json.Number preserves the exact source text instead of risking float
// rounding on re-encode.
type medusaPrice struct {
	CurrencyCode string      `json:"currency_code"`
	Amount       json.Number `json:"amount"`
}
type medusaVariant struct {
	ID     string        `json:"id"`
	SKU    string        `json:"sku"`
	Title  string        `json:"title"`
	Prices []medusaPrice `json:"prices"`
}
type medusaProduct struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Status      string          `json:"status"`
	Description string          `json:"description"`
	UpdatedAt   string          `json:"updated_at"`
	Variants    []medusaVariant `json:"variants"`
}
type medusaOrderItem struct {
	ID        string      `json:"id"`
	VariantID string      `json:"variant_id"`
	ProductID string      `json:"product_id"`
	Quantity  json.Number `json:"quantity"`
}
type medusaOrder struct {
	ID           string            `json:"id"`
	DisplayID    json.Number       `json:"display_id"`
	Status       string            `json:"status"`
	CurrencyCode string            `json:"currency_code"`
	CreatedAt    string            `json:"created_at"`
	UpdatedAt    string            `json:"updated_at"`
	Items        []medusaOrderItem `json:"items"`
}
type medusaInventoryLevel struct {
	LocationID        string      `json:"location_id"`
	StockedQuantity   json.Number `json:"stocked_quantity"`
	AvailableQuantity json.Number `json:"available_quantity"`
}
type medusaInventoryItem struct {
	ID             string                 `json:"id"`
	SKU            string                 `json:"sku"`
	LocationLevels []medusaInventoryLevel `json:"location_levels"`
}
type medusaStockLocation struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type medusaReturn struct {
	ID           string      `json:"id"`
	Status       string      `json:"status"`
	OrderID      string      `json:"order_id"`
	RefundAmount json.Number `json:"refund_amount"`
	CreatedAt    string      `json:"created_at"`
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
