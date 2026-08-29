package shopify

import (
	"context"
	"encoding/json"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func (connector *Connector) ReadProducts(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ProductPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || request.Validate(50) != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ProductPage{}, err
	}
	fingerprint := configuration.fingerprint("products")
	pageInfo, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.ProductPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		response, callErr := connector.call(ctx, configuration, credential, "GET", "/products.json", listQuery(pageInfo, request.Limit), nil)
		if callErr != nil {
			return callErr
		}
		var page struct {
			Products []shopifyProduct `json:"products"`
		}
		if json.Unmarshal(response.Body, &page) != nil || len(page.Products) > request.Limit {
			return ErrInvalidResponse
		}
		result := make([]sdk.RemoteProduct, 0, len(page.Products))
		for _, product := range page.Products {
			if product.ID < 1 || !validRemoteText(product.Title, 500) || len(product.Variants) == 0 {
				return ErrInvalidResponse
			}
			updated, parseErr := time.Parse(time.RFC3339, product.UpdatedAt)
			if parseErr != nil {
				return ErrInvalidResponse
			}
			brand := product.Vendor
			if brand != "" && !validRemoteText(brand, 300) {
				return ErrInvalidResponse
			}
			variants := make([]sdk.RemoteVariant, 0, len(product.Variants))
			sellerSKU := ""
			for _, variant := range product.Variants {
				if variant.ID < 1 || (variant.SKU != "" && !validRemoteText(variant.SKU, 200)) {
					return ErrInvalidResponse
				}
				if sellerSKU == "" {
					sellerSKU = variant.SKU
				}
				variants = append(variants, sdk.RemoteVariant{RemoteID: variantRemoteID(variant.ID), SKUs: []string{variant.SKU}})
			}
			item := sdk.RemoteProduct{RemoteID: intString(product.ID), SellerSKU: sellerSKU, Title: product.Title, Brand: brand, UpdatedAt: updated.UTC(), Variants: variants}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			result = append(result, item)
		}
		next, nextErr := nextCursor(response.NextPageInfo, fingerprint)
		if nextErr != nil {
			return nextErr
		}
		output = sdk.ProductPage{Items: result, NextCursor: next}
		return output.Validate(request.Limit)
	})
	return output, err
}
