package shopware

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func priceProjection(remoteID, value, currency string, updatedAt time.Time) (sdk.RemotePrice, error) {
	item := sdk.RemotePrice{VariantRemoteID: remoteID, Value: value, Currency: currency, UpdatedAt: updatedAt}
	if item.Validate() != nil {
		return sdk.RemotePrice{}, ErrInvalidResponse
	}
	return item, nil
}

func priceForCurrency(prices []shopwarePrice, currencyID string) (string, bool) {
	for _, price := range prices {
		if price.CurrencyID == currencyID {
			return price.Gross.String(), true
		}
	}
	return "", false
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
	page, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.PricePage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		currencyID, currencyErr := connector.currencyID(ctx, configuration, account.ID, credential)
		if currencyErr != nil {
			return currencyErr
		}
		products, total, callErr := connector.listTopLevelProducts(ctx, configuration, account.ID, credential, page, request.Limit)
		if callErr != nil {
			return callErr
		}
		items := []sdk.RemotePrice{}
		project := func(row shopwareProduct) error {
			updated, parseErr := time.Parse(time.RFC3339, row.UpdatedAt)
			if parseErr != nil {
				return ErrInvalidResponse
			}
			value, found := priceForCurrency(row.Price, currencyID)
			if !found {
				return nil
			}
			item, projectErr := priceProjection(row.ID, value, configuration.StoreCurrency, updated.UTC())
			if projectErr != nil {
				return projectErr
			}
			items = append(items, item)
			return nil
		}
		for _, product := range products {
			if product.ChildCount > 0 {
				children, variantErr := connector.listVariants(ctx, configuration, account.ID, credential, product.ID)
				if variantErr != nil {
					return variantErr
				}
				for _, child := range children {
					if e := project(child); e != nil {
						return e
					}
				}
			} else if e := project(product); e != nil {
				return e
			}
			if len(items) > 5000 {
				return ErrInvalidResponse
			}
		}
		next, nextErr := nextCursor(page, request.Limit, total, fingerprint)
		if nextErr != nil {
			return nextErr
		}
		output = sdk.PricePage{Items: items, NextCursor: next}
		return output.Validate(5000)
	})
	return output, err
}
