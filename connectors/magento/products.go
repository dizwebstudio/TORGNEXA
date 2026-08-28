package magento

import (
	"context"
	"encoding/json"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// pathSKU percent-encodes a SKU for use as a URL path segment: Magento
// allows spaces and other punctuation in a SKU, unlike the UUID-shaped
// identifiers other storefront connectors address by. Implemented by hand
// (RFC 3986 unreserved characters pass through, everything else is escaped)
// because provider packages may not import net/url or any other "net/*"
// package -- outbound networking is host-injected through the Connector SDK
// transport boundary, not performed by connector code directly.
func pathSKU(sku string) string {
	const hex = "0123456789ABCDEF"
	var out []byte
	for i := 0; i < len(sku); i++ {
		c := sku[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			out = append(out, c)
		default:
			out = append(out, '%', hex[c>>4], hex[c&0x0f])
		}
	}
	return string(out)
}

func (connector *Connector) listProducts(ctx context.Context, configuration Configuration, credential credentials, page, pageSize int) ([]magentoProduct, int, error) {
	query := searchCriteriaQuery(page, pageSize, nil)
	response, err := connector.call(ctx, configuration, credential, "GET", "/products", query, nil)
	if err != nil {
		return nil, 0, err
	}
	var result struct {
		Items      []magentoProduct `json:"items"`
		TotalCount int              `json:"total_count"`
	}
	if json.Unmarshal(response.Body, &result) != nil || len(result.Items) > pageSize {
		return nil, 0, ErrInvalidResponse
	}
	return result.Items, result.TotalCount, nil
}

// ReadProducts projects each Magento product row as its own single-variant
// sdk.RemoteProduct: Magento's configurable-product variant relationship
// (a parent "configurable" product linked to child "simple" products via a
// separate extension attribute) is not modeled specially here, the same
// flattened simplification WooCommerce/Shopware already make for their own
// variant models -- every SKU in the flat product list, parent or child,
// appears as its own row.
func (connector *Connector) ReadProducts(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ProductPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || request.Validate(50) != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ProductPage{}, err
	}
	fingerprint := configuration.fingerprint("products")
	page, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.ProductPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		products, total, callErr := connector.listProducts(ctx, configuration, credential, page, request.Limit)
		if callErr != nil {
			return callErr
		}
		result := make([]sdk.RemoteProduct, 0, len(products))
		for _, product := range products {
			if !validRemoteText(product.SKU, 200) || !validRemoteText(product.Name, 500) {
				return ErrInvalidResponse
			}
			updated, parseErr := parseMagentoTime(product.UpdatedAt)
			if parseErr != nil {
				return ErrInvalidResponse
			}
			variant := sdk.RemoteVariant{RemoteID: product.SKU, SKUs: []string{product.SKU}}
			item := sdk.RemoteProduct{RemoteID: product.SKU, SellerSKU: product.SKU, Title: product.Name, UpdatedAt: updated.UTC(), Variants: []sdk.RemoteVariant{variant}}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			result = append(result, item)
		}
		next, nextErr := nextCursor(page, request.Limit, total, fingerprint)
		if nextErr != nil {
			return nextErr
		}
		output = sdk.ProductPage{Items: result, NextCursor: next}
		return output.Validate(request.Limit)
	})
	return output, err
}
