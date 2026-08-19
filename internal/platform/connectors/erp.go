package connectors

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

var ErrInvalidERPRead = errors.New("connectors: invalid erp read projection")

var exactDecimalPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,9})?$`)

// ERPProduct is a provider-neutral catalog projection for ERP read connectors.
// Revision is the remote optimistic/version token (for 1C OData this is usually
// DataVersion). Provider-specific keys remain remote identities and are linked
// to canonical entities only through Task-010 EntityMapping.
type ERPProduct struct {
	RemoteID string `json:"remote_id"`
	Code     string `json:"code"`
	SKU      string `json:"sku,omitempty"`
	Title    string `json:"title"`
	Brand    string `json:"brand,omitempty"`
	Revision string `json:"revision"`
	Archived bool   `json:"archived"`
}

func (product ERPProduct) Validate() error {
	if !validRemoteReadID(product.RemoteID) || !validOptionalReadText(product.Code, 200) ||
		!validOptionalReadText(product.SKU, 200) || !validReadText(product.Title, 500) ||
		!validOptionalReadText(product.Brand, 300) || !validRemoteRevision(product.Revision) {
		return ErrInvalidERPRead
	}
	return nil
}

type ERPCatalogPage struct {
	Items      []ERPProduct `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

func (page ERPCatalogPage) Validate(maxItems int) error {
	if maxItems < 1 || len(page.Items) > maxItems || len(page.NextCursor) > 4096 || !utf8.ValidString(page.NextCursor) {
		return ErrInvalidERPRead
	}
	seen := make(map[string]struct{}, len(page.Items))
	for _, item := range page.Items {
		if item.Validate() != nil {
			return ErrInvalidERPRead
		}
		if _, duplicate := seen[item.RemoteID]; duplicate {
			return ErrInvalidERPRead
		}
		seen[item.RemoteID] = struct{}{}
	}
	return nil
}

// ERPCatalogReader is the additive SDK-v1 capability surface for erp.catalog.read.
type ERPCatalogReader interface {
	ReadERPCatalog(context.Context, Account, Runtime, PageRequest) (ERPCatalogPage, error)
}

// ERPInventory is a flattened exact inventory balance. Quantity is a canonical
// decimal string so fractional ERP quantities never pass through float64.
type ERPInventory struct {
	LocationRemoteID string `json:"location_remote_id"`
	ProductRemoteID  string `json:"product_remote_id"`
	Quantity         string `json:"quantity"`
}

func (item ERPInventory) Validate() error {
	if !validRemoteReadID(item.LocationRemoteID) || !validRemoteReadID(item.ProductRemoteID) || !validExactDecimal(item.Quantity) {
		return ErrInvalidERPRead
	}
	return nil
}

type ERPInventoryPage struct {
	Items      []ERPInventory `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

func (page ERPInventoryPage) Validate(maxItems int) error {
	if maxItems < 1 || len(page.Items) > maxItems || len(page.NextCursor) > 4096 || !utf8.ValidString(page.NextCursor) {
		return ErrInvalidERPRead
	}
	seen := make(map[string]struct{}, len(page.Items))
	for _, item := range page.Items {
		if item.Validate() != nil {
			return ErrInvalidERPRead
		}
		key := item.LocationRemoteID + "\x00" + item.ProductRemoteID
		if _, duplicate := seen[key]; duplicate {
			return ErrInvalidERPRead
		}
		seen[key] = struct{}{}
	}
	return nil
}

// ERPInventoryReader is the additive SDK-v1 capability surface for erp.inventory.read.
type ERPInventoryReader interface {
	ReadERPInventory(context.Context, Account, Runtime, PageRequest) (ERPInventoryPage, error)
}

func validRemoteRevision(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 256 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validExactDecimal(value string) bool {
	return exactDecimalPattern.MatchString(value)
}

// ERPOrder is the minimal provider-neutral customer-order projection used by
// read-only ERP connectors. Remote status/location identities stay external
// mapping keys; Revision is the remote optimistic/version evidence.
type ERPOrder struct {
	RemoteID         string `json:"remote_id"`
	Number           string `json:"number"`
	Revision         string `json:"revision"`
	StatusRemoteID   string `json:"status_remote_id,omitempty"`
	LocationRemoteID string `json:"location_remote_id,omitempty"`
	Applicable       bool   `json:"applicable"`
	Deleted          bool   `json:"deleted"`
}

func (order ERPOrder) Validate() error {
	if !validRemoteReadID(order.RemoteID) || !validReadText(order.Number, 300) || !validRemoteRevision(order.Revision) ||
		!validOptionalRemoteReadID(order.StatusRemoteID) || !validOptionalRemoteReadID(order.LocationRemoteID) {
		return ErrInvalidERPRead
	}
	return nil
}

type ERPOrderPage struct {
	Items      []ERPOrder `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

func (page ERPOrderPage) Validate(maxItems int) error {
	if maxItems < 1 || len(page.Items) > maxItems || len(page.NextCursor) > 4096 || !utf8.ValidString(page.NextCursor) {
		return ErrInvalidERPRead
	}
	seen := make(map[string]struct{}, len(page.Items))
	for _, item := range page.Items {
		if item.Validate() != nil {
			return ErrInvalidERPRead
		}
		if _, duplicate := seen[item.RemoteID]; duplicate {
			return ErrInvalidERPRead
		}
		seen[item.RemoteID] = struct{}{}
	}
	return nil
}

// ERPOrderReader is the additive SDK-v1 capability surface for erp.orders.read.
type ERPOrderReader interface {
	ReadERPOrders(context.Context, Account, Runtime, PageRequest) (ERPOrderPage, error)
}

func validOptionalRemoteReadID(value string) bool {
	return value == "" || validRemoteReadID(value)
}
