package cscart

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// ReadPrices reads the base price and optional manufacturer list price from
// the CS-Cart product projection. CS-Cart exposes these values as strings in
// the product response; product IDs are used as the remote variant identity
// because this adapter deliberately does not claim option-combination support.
func (c *Connector) ReadPrices(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.PricePage, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.read") != nil || request.Validate(100) != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.PricePage{}, err
	}
	fingerprint := cfg.fingerprint("prices")
	page, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}

	var output sdk.PricePage
	err = c.withCredentials(ctx, runtime, account.SecretReference, func(cred credentials) error {
		products, total, listErr := c.listProducts(ctx, cfg, cred, page, request.Limit)
		if listErr != nil {
			return listErr
		}
		items := make([]sdk.RemotePrice, 0, len(products))
		for _, product := range products {
			id, idErr := productID(product)
			updated, timeErr := productUpdatedAt(product)
			if idErr != nil || timeErr != nil || !productValid(product) {
				return ErrInvalidResponse
			}
			item := sdk.RemotePrice{
				VariantRemoteID: id,
				Value:           product.Price,
				CompareAt:       product.ListPrice,
				Currency:        cfg.StoreCurrency,
				UpdatedAt:       updated,
			}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		if len(products) == request.Limit && (total == 0 || page*request.Limit < total) {
			cursor, cursorErr := encodePageCursor(page+1, fingerprint)
			if cursorErr != nil {
				return cursorErr
			}
			output.NextCursor = cursor
		}
		output.Items = items
		return output.Validate(request.Limit)
	})
	return output, err
}
