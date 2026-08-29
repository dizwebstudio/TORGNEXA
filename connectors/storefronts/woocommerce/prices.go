package woocommerce

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func priceProjection(remoteID, price, regular, sale, currency string, updatedAt time.Time) (sdk.RemotePrice, error) {
	value := price
	compare := ""
	if value == "" {
		value = regular
	}
	if sale != "" {
		value = sale
		if regular != "" && regular != sale {
			compare = regular
		}
	}
	item := sdk.RemotePrice{VariantRemoteID: remoteID, Value: value, CompareAt: compare, Currency: currency, UpdatedAt: updatedAt}
	if item.Validate() != nil {
		return sdk.RemotePrice{}, ErrInvalidResponse
	}
	return item, nil
}

func (connector *Connector) ReadPrices(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.PricePage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.read") != nil || request.Validate(50) != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.PricePage{}, err
	}
	page, err := decodePageCursor(request.Cursor, configuration.fingerprint("prices"))
	if err != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.PricePage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		products, response, callErr := connector.listProducts(ctx, configuration, credential, page, request.Limit)
		if callErr != nil {
			return callErr
		}
		items := []sdk.RemotePrice{}
		for _, product := range products {
			updated, parseErr := parseWooTime(product.DateModifiedGMT)
			if parseErr != nil {
				return parseErr
			}
			if product.Type == "variable" {
				variations, variationErr := connector.listVariations(ctx, configuration, credential, product.ID)
				if variationErr != nil {
					return variationErr
				}
				for _, variation := range variations {
					variationUpdated, e := parseWooTime(variation.DateModifiedGMT)
					if e != nil {
						return e
					}
					item, e := priceProjection(variantRemoteID(product.ID, variation.ID), variation.Price, variation.RegularPrice, variation.SalePrice, configuration.StoreCurrency, variationUpdated)
					if e != nil {
						return e
					}
					items = append(items, item)
				}
			} else {
				item, e := priceProjection(variantRemoteID(product.ID, 0), product.Price, product.RegularPrice, product.SalePrice, configuration.StoreCurrency, updated)
				if e != nil {
					return e
				}
				items = append(items, item)
				if len(items) > 5000 {
					return ErrInvalidResponse
				}
			}
			if len(items) > 5000 {
				return ErrInvalidResponse
			}
		}
		next, nextErr := nextCursor(page, request.Limit, len(products), response.TotalPages, configuration.fingerprint("prices"))
		if nextErr != nil {
			return nextErr
		}
		output = sdk.PricePage{Items: items, NextCursor: next}
		// A variable product can expand to more price rows than product page limit;
		// validate against the documented bounded expansion rather than PageRequest.Limit.
		return output.Validate(5000)
	})
	return output, err
}
