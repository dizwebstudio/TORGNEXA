package cscart

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

type csCartProduct struct {
	ProductID       json.RawMessage `json:"product_id"`
	Product         string          `json:"product"`
	ProductCode     string          `json:"product_code"`
	Status          string          `json:"status"`
	FullDescription string          `json:"full_description"`
	UpdatedUnix     json.RawMessage `json:"updated_timestamp"`
}

type productListResponse struct {
	Products []csCartProduct `json:"products"`
	Params   struct {
		TotalItems json.RawMessage `json:"total_items"`
	} `json:"params"`
}

func rawString(raw json.RawMessage) (string, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return "", ErrInvalidResponse
	}
	if value[0] == '"' {
		var result string
		if json.Unmarshal(raw, &result) != nil {
			return "", ErrInvalidResponse
		}
		return result, nil
	}
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return "", ErrInvalidResponse
	}
	return value, nil
}

func productID(product csCartProduct) (string, error) {
	id, err := rawString(product.ProductID)
	if err != nil {
		return "", err
	}
	if parsed, err := strconv.ParseInt(id, 10, 64); err != nil || parsed < 1 {
		return "", ErrInvalidResponse
	}
	return id, nil
}

func productUpdatedAt(product csCartProduct) (time.Time, error) {
	value, err := rawString(product.UpdatedUnix)
	if err != nil {
		return time.Time{}, err
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds < 1 {
		return time.Time{}, ErrInvalidResponse
	}
	return time.Unix(seconds, 0).UTC(), nil
}

func productValid(product csCartProduct) bool {
	return product.ProductCode != "" && product.Product != "" && (product.Status == "A" || product.Status == "D" || product.Status == "H")
}
