package magento

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func (connector *Connector) ReadPrices(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.PricePage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.read") != nil || request.Validate(50) != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.PricePage{}, err
	}
	fingerprint := configuration.fingerprint("prices")
	page, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.PricePage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		products, total, callErr := connector.listProducts(ctx, configuration, credential, page, request.Limit)
		if callErr != nil {
			return callErr
		}
		items := make([]sdk.RemotePrice, 0, len(products))
		for _, product := range products {
			updated, parseErr := parseMagentoTime(product.UpdatedAt)
			if parseErr != nil {
				return ErrInvalidResponse
			}
			item := sdk.RemotePrice{VariantRemoteID: product.SKU, Value: product.Price.String(), Currency: configuration.StoreCurrency, UpdatedAt: updated.UTC()}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, nextErr := nextCursor(page, request.Limit, total, fingerprint)
		if nextErr != nil {
			return nextErr
		}
		output = sdk.PricePage{Items: items, NextCursor: next}
		return output.Validate(request.Limit)
	})
	return output, err
}
