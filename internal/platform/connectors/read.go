package connectors

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalidReadRequest = errors.New("connectors: invalid read request")

// PageRequest is a provider-neutral bounded cursor request. Cursor is opaque to
// the host and must only be interpreted by the connector that produced it.
type PageRequest struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit"`
}

func (request PageRequest) Validate(maxLimit int) error {
	if maxLimit < 1 || request.Limit < 1 || request.Limit > maxLimit || len(request.Cursor) > 4096 || !utf8.ValidString(request.Cursor) {
		return ErrInvalidReadRequest
	}
	return nil
}

// RemoteProduct is the bounded canonical read projection returned by a
// marketplace/classified connector. Provider-specific IDs remain remote IDs;
// they are linked to Core only through EntityMapping.
type RemoteProduct struct {
	RemoteID  string          `json:"remote_id"`
	SellerSKU string          `json:"seller_sku"`
	Title     string          `json:"title"`
	Brand     string          `json:"brand,omitempty"`
	UpdatedAt time.Time       `json:"updated_at"`
	Variants  []RemoteVariant `json:"variants,omitempty"`
}

type RemoteVariant struct {
	RemoteID string   `json:"remote_id"`
	SKUs     []string `json:"skus,omitempty"`
}

func (product RemoteProduct) Validate() error {
	if !validRemoteReadID(product.RemoteID) || !validReadText(product.SellerSKU, 200) || !validReadText(product.Title, 500) || !validOptionalReadText(product.Brand, 300) || product.UpdatedAt.IsZero() || product.UpdatedAt.Location() != time.UTC || len(product.Variants) > 1000 {
		return ErrInvalidReadRequest
	}
	seen := make(map[string]struct{}, len(product.Variants))
	for _, variant := range product.Variants {
		if !validRemoteReadID(variant.RemoteID) || len(variant.SKUs) > 100 {
			return ErrInvalidReadRequest
		}
		if _, duplicate := seen[variant.RemoteID]; duplicate {
			return ErrInvalidReadRequest
		}
		seen[variant.RemoteID] = struct{}{}
		for _, sku := range variant.SKUs {
			if !validReadText(sku, 200) {
				return ErrInvalidReadRequest
			}
		}
	}
	return nil
}

type ProductPage struct {
	Items      []RemoteProduct `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

func (page ProductPage) Validate(maxItems int) error {
	if maxItems < 1 || len(page.Items) > maxItems || len(page.NextCursor) > 4096 || !utf8.ValidString(page.NextCursor) {
		return ErrInvalidReadRequest
	}
	seen := make(map[string]struct{}, len(page.Items))
	for _, item := range page.Items {
		if err := item.Validate(); err != nil {
			return err
		}
		if _, duplicate := seen[item.RemoteID]; duplicate {
			return ErrInvalidReadRequest
		}
		seen[item.RemoteID] = struct{}{}
	}
	return nil
}

// ProductReader is the additive SDK-v1 capability surface for products.read.
type ProductReader interface {
	ReadProducts(context.Context, Account, Runtime, PageRequest) (ProductPage, error)
}

type RemoteLocation struct {
	RemoteID string `json:"remote_id"`
	Name     string `json:"name"`
}

func (location RemoteLocation) Validate() error {
	if !validRemoteReadID(location.RemoteID) || !validReadText(location.Name, 300) {
		return ErrInvalidReadRequest
	}
	return nil
}

type InventoryQuery struct {
	LocationRemoteID string   `json:"location_remote_id"`
	VariantRemoteIDs []string `json:"variant_remote_ids"`
}

func (query InventoryQuery) Validate(maxVariants int) error {
	if !validRemoteReadID(query.LocationRemoteID) || maxVariants < 1 || len(query.VariantRemoteIDs) < 1 || len(query.VariantRemoteIDs) > maxVariants {
		return ErrInvalidReadRequest
	}
	seen := make(map[string]struct{}, len(query.VariantRemoteIDs))
	for _, id := range query.VariantRemoteIDs {
		if !validRemoteReadID(id) {
			return ErrInvalidReadRequest
		}
		if _, duplicate := seen[id]; duplicate {
			return ErrInvalidReadRequest
		}
		seen[id] = struct{}{}
	}
	return nil
}

type RemoteInventory struct {
	LocationRemoteID string `json:"location_remote_id"`
	VariantRemoteID  string `json:"variant_remote_id"`
	Quantity         int64  `json:"quantity"`
}

func (item RemoteInventory) Validate() error {
	if !validRemoteReadID(item.LocationRemoteID) || !validRemoteReadID(item.VariantRemoteID) || item.Quantity < 0 {
		return ErrInvalidReadRequest
	}
	return nil
}

// InventoryReader is the additive SDK-v1 capability surface for inventory.read.
type InventoryReader interface {
	ListInventoryLocations(context.Context, Account, Runtime) ([]RemoteLocation, error)
	ReadInventory(context.Context, Account, Runtime, InventoryQuery) ([]RemoteInventory, error)
}

func validRemoteReadID(value string) bool {
	return validRemoteID(value)
}

func validReadText(value string, max int) bool {
	if value == "" || value != strings.TrimSpace(value) || !utf8.ValidString(value) || utf8.RuneCountInString(value) > max {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validOptionalReadText(value string, max int) bool {
	return value == "" || validReadText(value, max)
}
