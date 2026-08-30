package cscart

import (
	"context"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const storeLocationID = "cs-cart-store"

// ListInventoryLocations exposes the single CS-Cart storefront stock balance.
// CS-Cart's product API does not expose warehouse-level balances in this
// adapter, so no synthetic per-warehouse locations are created.
func (c *Connector) ListInventoryLocations(ctx context.Context, account sdk.Account, runtime sdk.Runtime) ([]sdk.RemoteLocation, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	if _, err := c.configuration(ctx, account); err != nil {
		return nil, err
	}
	location := sdk.RemoteLocation{RemoteID: storeLocationID, Name: "CS-Cart storefront stock"}
	if location.Validate() != nil {
		return nil, ErrInvalidResponse
	}
	return []sdk.RemoteLocation{location}, nil
}

// ReadInventory reads integer product amounts from the CS-Cart product
// projection for the single storefront location. Fractional amounts are
// rejected instead of being rounded.
func (c *Connector) ReadInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.InventoryQuery) ([]sdk.RemoteInventory, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil || query.Validate(200) != nil || query.LocationRemoteID != storeLocationID {
		return nil, sdk.ErrInvalidReadRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return nil, err
	}
	output := make([]sdk.RemoteInventory, 0, len(query.VariantRemoteIDs))
	err = c.withCredentials(ctx, runtime, account.SecretReference, func(cred credentials) error {
		for _, remoteID := range query.VariantRemoteIDs {
			product, fetchErr := c.fetchProduct(ctx, cfg, cred, remoteID)
			if fetchErr != nil {
				return fetchErr
			}
			if !productValid(product) {
				return ErrInvalidResponse
			}
			quantity, parseErr := strconv.ParseInt(product.Amount, 10, 64)
			if parseErr != nil || quantity < 0 {
				return ErrInvalidResponse
			}
			item := sdk.RemoteInventory{LocationRemoteID: storeLocationID, VariantRemoteID: remoteID, Quantity: quantity}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			output = append(output, item)
		}
		return nil
	})
	return output, err
}
