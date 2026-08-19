package woocommerce

import (
	"strconv"
	"strings"
)

type variantTarget struct{ ProductID, VariationID int64 }

func parseVariantTarget(value string) (variantTarget, error) {
	parts := strings.Split(value, ":")
	if len(parts) == 2 && parts[0] == "product" {
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err == nil && id > 0 {
			return variantTarget{ProductID: id}, nil
		}
	}
	if len(parts) == 3 && parts[0] == "variation" {
		p, e1 := strconv.ParseInt(parts[1], 10, 64)
		v, e2 := strconv.ParseInt(parts[2], 10, 64)
		if e1 == nil && e2 == nil && p > 0 && v > 0 {
			return variantTarget{ProductID: p, VariationID: v}, nil
		}
	}
	return variantTarget{}, ErrInvalidResponse
}
func (target variantTarget) path() string {
	if target.VariationID > 0 {
		return "/products/" + intString(target.ProductID) + "/variations/" + intString(target.VariationID)
	}
	return "/products/" + intString(target.ProductID)
}
