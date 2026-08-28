package bitrix

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type bitrixProduct struct {
	ID          int64       `json:"id"`
	IblockID    int64       `json:"iblockId"`
	Name        string      `json:"name"`
	Active      string      `json:"active"`
	Code        string      `json:"code"`
	XMLID       string      `json:"xmlId"`
	DetailText  string      `json:"detailText"`
	Quantity    json.Number `json:"quantity"`
	TimestampX  string      `json:"timestampX"`
	DateCreated string      `json:"dateCreate"`
}

type bitrixProductList struct {
	Products []bitrixProduct `json:"products"`
}

func decodeProductList(body []byte) ([]bitrixProduct, error) {
	var envelope struct {
		Result *bitrixProductList `json:"result"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Result == nil || len(envelope.Result.Products) > 50 {
		return nil, ErrInvalidResponse
	}
	return envelope.Result.Products, nil
}

func parseBitrixTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("bitrix: missing timestamp")
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), nil
	}
	if parsed, err := time.Parse("2006-01-02T15:04:05-07:00", value); err == nil {
		return parsed.UTC(), nil
	}
	return time.Time{}, errors.New("bitrix: invalid timestamp")
}

func productSKU(product bitrixProduct) string {
	if product.XMLID != "" {
		return product.XMLID
	}
	if product.Code != "" {
		return product.Code
	}
	return strconv.FormatInt(product.ID, 10)
}

func productStatus(active string) string {
	if strings.EqualFold(active, "Y") {
		return "publish"
	}
	return "draft"
}

func activeValue(status string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "publish", "enabled", "active", "y":
		return "Y", true
	case "draft", "private", "disabled", "archived", "n":
		return "N", true
	default:
		return "", false
	}
}

func productUpdatedAt(product bitrixProduct) (time.Time, error) {
	if product.TimestampX != "" {
		return parseBitrixTime(product.TimestampX)
	}
	return parseBitrixTime(product.DateCreated)
}

func validRemoteText(value string, max int) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > max {
		return false
	}
	for _, r := range value {
		if r == 0 || r == 0x7f {
			return false
		}
	}
	return true
}
