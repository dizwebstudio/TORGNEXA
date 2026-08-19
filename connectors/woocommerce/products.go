package woocommerce

import (
	"context"
	"encoding/json"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func (connector *Connector) listProducts(ctx context.Context, configuration Configuration, credential credentials, page, limit int) ([]wooProduct, Response, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/products", []QueryParam{{Name: "page", Value: intString(int64(page))}, {Name: "per_page", Value: intString(int64(limit))}, {Name: "orderby", Value: "modified"}, {Name: "order", Value: "asc"}}, nil)
	if err != nil {
		return nil, response, err
	}
	var products []wooProduct
	if json.Unmarshal(response.Body, &products) != nil || len(products) > limit {
		return nil, response, ErrInvalidResponse
	}
	return products, response, nil
}

func (connector *Connector) listVariations(ctx context.Context, configuration Configuration, credential credentials, productID int64) ([]wooVariation, error) {
	var all []wooVariation
	for page := 1; page <= 10; page++ {
		response, err := connector.call(ctx, configuration, credential, "GET", "/products/"+intString(productID)+"/variations", []QueryParam{{Name: "page", Value: intString(int64(page))}, {Name: "per_page", Value: "100"}, {Name: "orderby", Value: "id"}, {Name: "order", Value: "asc"}}, nil)
		if err != nil {
			return nil, err
		}
		var items []wooVariation
		if json.Unmarshal(response.Body, &items) != nil || len(items) > 100 {
			return nil, ErrInvalidResponse
		}
		all = append(all, items...)
		if len(all) > 1000 {
			return nil, ErrInvalidResponse
		}
		if response.TotalPages > 0 {
			if page >= response.TotalPages {
				break
			}
		} else if len(items) < 100 {
			break
		}
		if page == 10 {
			return nil, ErrInvalidResponse
		}
	}
	return all, nil
}

func (connector *Connector) ReadProducts(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ProductPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || request.Validate(50) != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ProductPage{}, err
	}
	page, err := decodePageCursor(request.Cursor, configuration.fingerprint("products"))
	if err != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.ProductPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		products, response, callErr := connector.listProducts(ctx, configuration, credential, page, request.Limit)
		if callErr != nil {
			return callErr
		}
		result := make([]sdk.RemoteProduct, 0, len(products))
		for _, product := range products {
			if product.ID < 1 || !validRemoteText(product.Name, 500) {
				return ErrInvalidResponse
			}
			updated, parseErr := parseWooTime(product.DateModifiedGMT)
			if parseErr != nil {
				return parseErr
			}
			brand := ""
			if len(product.Brands) > 0 {
				brand = product.Brands[0].Name
				if !validRemoteText(brand, 300) {
					return ErrInvalidResponse
				}
			}
			variants := []sdk.RemoteVariant{}
			sellerSKU := product.SKU
			switch product.Type {
			case "simple", "external":
				if !validRemoteText(product.SKU, 200) {
					return ErrInvalidResponse
				}
				variants = append(variants, sdk.RemoteVariant{RemoteID: variantRemoteID(product.ID, 0), SKUs: []string{product.SKU}})
			case "variable":
				remoteVariants, variationErr := connector.listVariations(ctx, configuration, credential, product.ID)
				if variationErr != nil {
					return variationErr
				}
				for _, variation := range remoteVariants {
					if variation.ID < 1 || !validRemoteText(variation.SKU, 200) {
						return ErrInvalidResponse
					}
					if sellerSKU == "" {
						sellerSKU = variation.SKU
					}
					variants = append(variants, sdk.RemoteVariant{RemoteID: variantRemoteID(product.ID, variation.ID), SKUs: []string{variation.SKU}})
				}
				if len(variants) == 0 || !validRemoteText(sellerSKU, 200) {
					return ErrInvalidResponse
				}
			default:
				return ErrInvalidResponse
			}
			item := sdk.RemoteProduct{RemoteID: intString(product.ID), SellerSKU: sellerSKU, Title: product.Name, Brand: brand, UpdatedAt: updated, Variants: variants}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			result = append(result, item)
		}
		next, nextErr := nextCursor(page, request.Limit, len(products), response.TotalPages, configuration.fingerprint("products"))
		if nextErr != nil {
			return nextErr
		}
		output = sdk.ProductPage{Items: result, NextCursor: next}
		return output.Validate(request.Limit)
	})
	return output, err
}
