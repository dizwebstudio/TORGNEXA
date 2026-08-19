package prestashop

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

type scalar string

func (s *scalar) UnmarshalJSON(data []byte) error {
	var str string
	if json.Unmarshal(data, &str) == nil {
		*s = scalar(str)
		return nil
	}
	var n json.Number
	if json.Unmarshal(data, &n) == nil {
		*s = scalar(n.String())
		return nil
	}
	return ErrInvalidResponse
}
func (s scalar) String() string { return string(s) }
func (s scalar) Int64() (int64, error) {
	v, err := strconv.ParseInt(string(s), 10, 64)
	if err != nil || v < 0 {
		return 0, ErrInvalidResponse
	}
	return v, nil
}

type psProduct struct {
	ID        scalar `json:"id"`
	Reference string `json:"reference"`
	Name      any    `json:"name"`
	Price     string `json:"price"`
	Active    scalar `json:"active"`
	DateUpd   string `json:"date_upd"`
}
type psCombination struct {
	ID        scalar `json:"id"`
	ProductID scalar `json:"id_product"`
	Reference string `json:"reference"`
	Price     string `json:"price"`
	DateUpd   string `json:"date_upd"`
}
type psStock struct {
	ID          scalar `json:"id"`
	ProductID   scalar `json:"id_product"`
	AttributeID scalar `json:"id_product_attribute"`
	Quantity    scalar `json:"quantity"`
}
type psOrder struct {
	ID           scalar `json:"id"`
	Reference    string `json:"reference"`
	CurrentState scalar `json:"current_state"`
	DateAdd      string `json:"date_add"`
	DateUpd      string `json:"date_upd"`
}
type psOrderDetail struct {
	ID          scalar `json:"id"`
	OrderID     scalar `json:"id_order"`
	ProductID   scalar `json:"product_id"`
	AttributeID scalar `json:"product_attribute_id"`
	Quantity    scalar `json:"product_quantity"`
}

func parsePSTime(value string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.UTC)
	if err != nil {
		if t2, e2 := time.Parse(time.RFC3339, value); e2 == nil {
			return t2.UTC(), nil
		}
		return time.Time{}, ErrInvalidResponse
	}
	return t.UTC(), nil
}
func localizedString(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if s, ok := m["value"].(string); ok && s != "" {
					return s, nil
				}
			}
		}
	case map[string]any:
		if s, ok := v["value"].(string); ok {
			return s, nil
		}
	}
	return "", ErrInvalidResponse
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
func variantRemoteID(productID, attributeID int64) string {
	if attributeID > 0 {
		return "combination:" + strconv.FormatInt(productID, 10) + ":" + strconv.FormatInt(attributeID, 10)
	}
	return "product:" + strconv.FormatInt(productID, 10)
}
