package opencart

import (
	"strconv"
	"strings"
	"time"
)

type bridgeVariant struct {
	RemoteID   string `json:"remote_id"`
	SKU        string `json:"sku"`
	Price      string `json:"price"`
	CompareAt  string `json:"compare_at"`
	Quantity   int64  `json:"quantity"`
	ModifiedAt string `json:"modified_at"`
}
type bridgeProduct struct {
	ID         int64           `json:"id"`
	SKU        string          `json:"sku"`
	Title      string          `json:"title"`
	Brand      string          `json:"brand"`
	Status     string          `json:"status"`
	Price      string          `json:"price"`
	CompareAt  string          `json:"compare_at"`
	Quantity   int64           `json:"quantity"`
	ModifiedAt string          `json:"modified_at"`
	Variants   []bridgeVariant `json:"variants"`
}
type bridgeOrderItem struct {
	ID              int64  `json:"id"`
	VariantRemoteID string `json:"variant_remote_id"`
	Quantity        int64  `json:"quantity"`
}
type bridgeOrder struct {
	ID             int64             `json:"id"`
	ExternalID     string            `json:"external_id"`
	StatusRemoteID string            `json:"status_remote_id"`
	CreatedAt      string            `json:"created_at"`
	UpdatedAt      string            `json:"updated_at"`
	Items          []bridgeOrderItem `json:"items"`
}

func parseTime(v string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339, v)
	if e != nil {
		return time.Time{}, ErrInvalidResponse
	}
	return t.UTC(), nil
}
func validText(v string, max int) bool {
	if v == "" || v != strings.TrimSpace(v) || len(v) > max {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func productVariantID(id int64) string { return "product:" + strconv.FormatInt(id, 10) }
