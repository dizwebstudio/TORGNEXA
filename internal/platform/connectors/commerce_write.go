package connectors

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrInvalidCommerceWrite = errors.New("connectors: invalid commerce write")

type CommerceWriteReceipt struct {
	RemoteID   string `json:"remote_id"`
	Applied    bool   `json:"applied"`
	Duplicate  bool   `json:"duplicate"`
	Reconciled bool   `json:"reconciled"`
}

func (receipt CommerceWriteReceipt) Validate() error {
	if !validRemoteID(receipt.RemoteID) || receipt.Applied == receipt.Duplicate || (receipt.Reconciled && !receipt.Applied && !receipt.Duplicate) {
		return ErrInvalidCommerceWrite
	}
	return nil
}

type ProductWriteRequest struct {
	RemoteID       string `json:"remote_id,omitempty"`
	SellerSKU      string `json:"seller_sku"`
	Title          string `json:"title"`
	Description    string `json:"description,omitempty"`
	StatusRemoteID string `json:"status_remote_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (request ProductWriteRequest) Validate() error {
	if request.RemoteID != "" && !validRemoteID(request.RemoteID) {
		return ErrInvalidCommerceWrite
	}
	// StatusRemoteID is a bounded remote-native status id, the same shape
	// OrderStatusWriteRequest.StatusRemoteID already uses: providers define
	// their own vocabulary (WooCommerce's draft/pending/private/publish,
	// Shopify's active/archived/draft, ...), so this validates shape, not a
	// fixed enum of any one provider's terms.
	if !validReadText(request.SellerSKU, 200) || !validReadText(request.Title, 500) || !validOptionalWriteText(request.Description, 10000) || !validIdempotencyKey(request.IdempotencyKey) || !validReadText(request.StatusRemoteID, 64) {
		return ErrInvalidCommerceWrite
	}
	return nil
}

type ProductWriter interface {
	UpsertProduct(context.Context, Account, Runtime, ProductWriteRequest) (CommerceWriteReceipt, error)
}

type PriceWriteRequest struct {
	VariantRemoteID string `json:"variant_remote_id"`
	Value           string `json:"value"`
	CompareAt       string `json:"compare_at,omitempty"`
	Currency        string `json:"currency"`
	IdempotencyKey  string `json:"idempotency_key"`
}

func (request PriceWriteRequest) Validate() error {
	if !validRemoteID(request.VariantRemoteID) || !validUnsignedMoney(request.Value) || (request.CompareAt != "" && !validUnsignedMoney(request.CompareAt)) || !validCurrency(request.Currency) || !validIdempotencyKey(request.IdempotencyKey) {
		return ErrInvalidCommerceWrite
	}
	return nil
}

type PriceWriter interface {
	WritePrice(context.Context, Account, Runtime, PriceWriteRequest) (CommerceWriteReceipt, error)
}

type InventoryWriteRequest struct {
	VariantRemoteID  string `json:"variant_remote_id"`
	LocationRemoteID string `json:"location_remote_id,omitempty"`
	Quantity         int64  `json:"quantity"`
	IdempotencyKey   string `json:"idempotency_key"`
}

func (request InventoryWriteRequest) Validate() error {
	if !validRemoteID(request.VariantRemoteID) || !validOptionalRemoteID(request.LocationRemoteID) || request.Quantity < 0 || !validIdempotencyKey(request.IdempotencyKey) {
		return ErrInvalidCommerceWrite
	}
	return nil
}

type InventoryWriter interface {
	WriteInventory(context.Context, Account, Runtime, InventoryWriteRequest) (CommerceWriteReceipt, error)
}

type OrderStatusWriteRequest struct {
	OrderRemoteID  string `json:"order_remote_id"`
	StatusRemoteID string `json:"status_remote_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (request OrderStatusWriteRequest) Validate() error {
	if !validRemoteID(request.OrderRemoteID) || !validReadText(request.StatusRemoteID, 64) || !validIdempotencyKey(request.IdempotencyKey) {
		return ErrInvalidCommerceWrite
	}
	return nil
}

type OrderStatusWriter interface {
	WriteOrderStatus(context.Context, Account, Runtime, OrderStatusWriteRequest) (CommerceWriteReceipt, error)
}

func validIdempotencyKey(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 128 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x21 || r == 0x7f {
			return false
		}
	}
	return true
}

func validOptionalWriteText(value string, max int) bool {
	if value == "" {
		return true
	}
	if value != strings.TrimSpace(value) || !utf8.ValidString(value) || utf8.RuneCountInString(value) > max {
		return false
	}
	for _, r := range value {
		if r == 0 || r == 0x7f {
			return false
		}
	}
	return true
}

func validOptionalRemoteID(value string) bool {
	return value == "" || validRemoteID(value)
}
