package medusa

import (
	"context"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func priceProjection(remoteID, value, currency string, updatedAt time.Time) (sdk.RemotePrice, error) {
	item := sdk.RemotePrice{VariantRemoteID: remoteID, Value: value, Currency: strings.ToUpper(currency), UpdatedAt: updatedAt}
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
	offset, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.PricePage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		products, count, callErr := connector.listProducts(ctx, configuration, credential, offset, request.Limit)
		if callErr != nil {
			return callErr
		}
		items := []sdk.RemotePrice{}
		for _, product := range products {
			updated, parseErr := time.Parse(time.RFC3339, product.UpdatedAt)
			if parseErr != nil {
				return ErrInvalidResponse
			}
			for _, variant := range product.Variants {
				for _, price := range variant.Prices {
					if !strings.EqualFold(price.CurrencyCode, configuration.StoreCurrency) {
						continue
					}
					item, projectErr := priceProjection(variantRemoteID(product.ID, variant.ID), price.Amount.String(), price.CurrencyCode, updated.UTC())
					if projectErr != nil {
						return projectErr
					}
					items = append(items, item)
					break
				}
				if len(items) > 5000 {
					return ErrInvalidResponse
				}
			}
		}
		next, nextErr := nextCursor(offset, request.Limit, count, fingerprint)
		if nextErr != nil {
			return nextErr
		}
		output = sdk.PricePage{Items: items, NextCursor: next}
		return output.Validate(5000)
	})
	return output, err
}
