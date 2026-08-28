package magento

import (
	"encoding/json"
	"errors"
	"time"
)

// parseMagentoTime accepts Magento's actual REST timestamp format
// ("2026-08-12 07:00:00", a MySQL DATETIME with no timezone, stored and
// returned in UTC by default), not RFC3339 -- a real, well-documented
// Magento quirk. RFC3339 is also accepted defensively in case a
// customization changes the format.
func parseMagentoTime(value string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UTC(), nil
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.UTC)
	if err != nil {
		return time.Time{}, errors.New("magento: invalid timestamp")
	}
	return t, nil
}

// magentoProduct.Price uses json.Number rather than float64 to avoid float
// rounding on re-encode; Magento serializes it as a plain JSON number.
// Description lives in CustomAttributes (Magento's EAV model), not as a
// top-level field.
type magentoCustomAttribute struct {
	AttributeCode string `json:"attribute_code"`
	Value         string `json:"value"`
}
type magentoProduct struct {
	SKU              string                   `json:"sku"`
	Name             string                   `json:"name"`
	Price            json.Number              `json:"price"`
	Status           int                      `json:"status"`
	CreatedAt        string                   `json:"created_at"`
	UpdatedAt        string                   `json:"updated_at"`
	CustomAttributes []magentoCustomAttribute `json:"custom_attributes"`
}

func (product magentoProduct) description() string {
	for _, attribute := range product.CustomAttributes {
		if attribute.AttributeCode == "description" {
			return attribute.Value
		}
	}
	return ""
}

type magentoOrderItem struct {
	ItemID     json.Number `json:"item_id"`
	SKU        string      `json:"sku"`
	ProductID  json.Number `json:"product_id"`
	QtyOrdered json.Number `json:"qty_ordered"`
}
type magentoOrder struct {
	EntityID    json.Number        `json:"entity_id"`
	IncrementID string             `json:"increment_id"`
	Status      string             `json:"status"`
	CreatedAt   string             `json:"created_at"`
	UpdatedAt   string             `json:"updated_at"`
	Items       []magentoOrderItem `json:"items"`
}
type magentoStockItem struct {
	ItemID    json.Number `json:"item_id"`
	ProductID json.Number `json:"product_id"`
	Qty       json.Number `json:"qty"`
	IsInStock bool        `json:"is_in_stock"`
}
type magentoCreditmemo struct {
	EntityID   json.Number `json:"entity_id"`
	OrderID    json.Number `json:"order_id"`
	GrandTotal json.Number `json:"grand_total"`
	CreatedAt  string      `json:"created_at"`
}
