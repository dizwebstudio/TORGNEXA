package medusa

import (
	"context"
	"encoding/json"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const productListFields = "id,title,status,description,updated_at,variants.id,variants.sku,variants.title,variants.prices.amount,variants.prices.currency_code"

func (connector *Connector) listProducts(ctx context.Context, configuration Configuration, credential credentials, offset, limit int) ([]medusaProduct, int, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/products", []QueryParam{{Name: "offset", Value: intString(offset)}, {Name: "limit", Value: intString(limit)}, {Name: "order", Value: "updated_at"}, {Name: "fields", Value: productListFields}}, nil)
	if err != nil {
		return nil, 0, err
	}
	var page struct {
		Products []medusaProduct `json:"products"`
		Count    int             `json:"count"`
	}
	if json.Unmarshal(response.Body, &page) != nil || len(page.Products) > limit {
		return nil, 0, ErrInvalidResponse
	}
	return page.Products, page.Count, nil
}

func intString(v int) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

func (connector *Connector) ReadProducts(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ProductPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || request.Validate(50) != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ProductPage{}, err
	}
	fingerprint := configuration.fingerprint("products")
	offset, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.ProductPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		products, count, callErr := connector.listProducts(ctx, configuration, credential, offset, request.Limit)
		if callErr != nil {
			return callErr
		}
		result := make([]sdk.RemoteProduct, 0, len(products))
		for _, product := range products {
			if product.ID == "" || !validRemoteText(product.Title, 500) || len(product.Variants) == 0 {
				return ErrInvalidResponse
			}
			updated, parseErr := time.Parse(time.RFC3339, product.UpdatedAt)
			if parseErr != nil {
				return ErrInvalidResponse
			}
			variants := make([]sdk.RemoteVariant, 0, len(product.Variants))
			sellerSKU := ""
			for _, variant := range product.Variants {
				if variant.ID == "" || (variant.SKU != "" && !validRemoteText(variant.SKU, 200)) {
					return ErrInvalidResponse
				}
				if sellerSKU == "" {
					sellerSKU = variant.SKU
				}
				variants = append(variants, sdk.RemoteVariant{RemoteID: variantRemoteID(product.ID, variant.ID), SKUs: []string{variant.SKU}})
			}
			item := sdk.RemoteProduct{RemoteID: product.ID, SellerSKU: sellerSKU, Title: product.Title, UpdatedAt: updated.UTC(), Variants: variants}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			result = append(result, item)
		}
		next, nextErr := nextCursor(offset, request.Limit, count, fingerprint)
		if nextErr != nil {
			return nextErr
		}
		output = sdk.ProductPage{Items: result, NextCursor: next}
		return output.Validate(request.Limit)
	})
	return output, err
}
