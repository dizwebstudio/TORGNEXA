package woocommerce

import (
	"strings"
	"time"
)

type wooBrand struct {
	Name string `json:"name"`
}
type wooProduct struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	Status          string     `json:"status"`
	SKU             string     `json:"sku"`
	Type            string     `json:"type"`
	Price           string     `json:"price"`
	RegularPrice    string     `json:"regular_price"`
	SalePrice       string     `json:"sale_price"`
	ManageStock     bool       `json:"manage_stock"`
	StockQuantity   *int64     `json:"stock_quantity"`
	StockStatus     string     `json:"stock_status"`
	DateModifiedGMT string     `json:"date_modified_gmt"`
	Variations      []int64    `json:"variations"`
	Brands          []wooBrand `json:"brands"`
}
type wooVariation struct {
	ID              int64  `json:"id"`
	SKU             string `json:"sku"`
	Price           string `json:"price"`
	RegularPrice    string `json:"regular_price"`
	SalePrice       string `json:"sale_price"`
	ManageStock     any    `json:"manage_stock"`
	StockQuantity   *int64 `json:"stock_quantity"`
	StockStatus     string `json:"stock_status"`
	DateModifiedGMT string `json:"date_modified_gmt"`
}
type wooOrder struct {
	ID              int64  `json:"id"`
	Number          string `json:"number"`
	Status          string `json:"status"`
	DateCreatedGMT  string `json:"date_created_gmt"`
	DateModifiedGMT string `json:"date_modified_gmt"`
	LineItems       []struct {
		ID          int64 `json:"id"`
		ProductID   int64 `json:"product_id"`
		VariationID int64 `json:"variation_id"`
		Quantity    int64 `json:"quantity"`
	} `json:"line_items"`
}
type wooRefund struct {
	ID             int64  `json:"id"`
	DateCreatedGMT string `json:"date_created_gmt"`
	Amount         string `json:"amount"`
	Reason         string `json:"reason"`
}

func parseWooTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, ErrInvalidResponse
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UTC(), nil
	}
	t, err := time.ParseInLocation("2006-01-02T15:04:05", value, time.UTC)
	if err != nil {
		return time.Time{}, ErrInvalidResponse
	}
	return t.UTC(), nil
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
func variantRemoteID(productID, variationID int64) string {
	if variationID > 0 {
		return "variation:" + intString(productID) + ":" + intString(variationID)
	}
	return "product:" + intString(productID)
}
func intString(v int64) string { return strconvFormatInt(v) }
