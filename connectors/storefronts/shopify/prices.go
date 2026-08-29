package shopify

import (
	"context"
	"encoding/json"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func priceProjection(remoteID, price, compareAt, currency string, updatedAt time.Time) (sdk.RemotePrice, error) {
	item := sdk.RemotePrice{VariantRemoteID: remoteID, Value: price, CompareAt: compareAt, Currency: currency, UpdatedAt: updatedAt}
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
	fingerprint := configuration.fingerprint("prices")
	pageInfo, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.PricePage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		response, callErr := connector.call(ctx, configuration, credential, "GET", "/products.json", listQuery(pageInfo, request.Limit, QueryParam{Name: "fields", Value: "id,variants,updated_at"}), nil)
		if callErr != nil {
			return callErr
		}
		var page struct {
			Products []shopifyProduct `json:"products"`
		}
		if json.Unmarshal(response.Body, &page) != nil || len(page.Products) > request.Limit {
			return ErrInvalidResponse
		}
		items := []sdk.RemotePrice{}
		for _, product := range page.Products {
			for _, variant := range product.Variants {
				if variant.ID < 1 {
					return ErrInvalidResponse
				}
				updated, parseErr := time.Parse(time.RFC3339, variant.UpdatedAt)
				if parseErr != nil {
					return ErrInvalidResponse
				}
				item, projectErr := priceProjection(variantRemoteID(variant.ID), variant.Price, variant.CompareAtPrice, configuration.StoreCurrency, updated.UTC())
				if projectErr != nil {
					return projectErr
				}
				items = append(items, item)
				if len(items) > 5000 {
					return ErrInvalidResponse
				}
			}
		}
		next, nextErr := nextCursor(response.NextPageInfo, fingerprint)
		if nextErr != nil {
			return nextErr
		}
		output = sdk.PricePage{Items: items, NextCursor: next}
		// A product can expand to more price rows than the product page
		// limit (one per variant), so validate against the documented
		// bounded expansion rather than PageRequest.Limit.
		return output.Validate(5000)
	})
	return output, err
}
