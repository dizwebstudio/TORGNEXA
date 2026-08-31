package cscart

import (
	"context"
	"encoding/json"

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

// WritePrice updates the product-level CS-Cart price fields. CS-Cart has no
// separate offer resource in the API surface used by this connector, so the
// product ID is the only admitted variant identity.
func (c *Connector) WritePrice(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PriceWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	if request.Currency != cfg.StoreCurrency {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	var receipt sdk.CommerceWriteReceipt
	err = c.withCredentials(ctx, runtime, account.SecretReference, func(cred credentials) error {
		current, fetchErr := c.fetchProduct(ctx, cfg, cred, request.VariantRemoteID)
		if fetchErr != nil {
			return fetchErr
		}
		if current.Price == request.Value && current.ListPrice == request.CompareAt {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, marshalErr := json.Marshal(struct {
			Price     string `json:"price"`
			ListPrice string `json:"list_price"`
		}{Price: request.Value, ListPrice: request.CompareAt})
		if marshalErr != nil {
			return marshalErr
		}
		if _, callErr := c.call(ctx, cfg, cred, "PUT", "/products/"+request.VariantRemoteID, nil, body); callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			reconciled, reconcileErr := c.fetchProduct(ctx, cfg, cred, request.VariantRemoteID)
			if reconcileErr == nil && reconciled.Price == request.Value && reconciled.ListPrice == request.CompareAt {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		updated, fetchErr := c.fetchProduct(ctx, cfg, cred, request.VariantRemoteID)
		if fetchErr != nil || updated.Price != request.Value || updated.ListPrice != request.CompareAt {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
		return receipt.Validate()
	})
	return receipt, err
}

var _ sdk.PriceWriter = (*Connector)(nil)
