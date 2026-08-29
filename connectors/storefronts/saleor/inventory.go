package saleor

import (
	"context"
	"encoding/json"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// ListInventoryLocations resolves and validates the single admin-configured
// warehouse: unlike Magento's synthetic single-location stand-in (Magento's
// legacy CatalogInventory API has no location concept of its own to query),
// Saleor genuinely has multiple named warehouses, so this reports the real
// resolved warehouse id rather than a made-up constant.
func (connector *Connector) ListInventoryLocations(ctx context.Context, account sdk.Account, runtime sdk.Runtime) ([]sdk.RemoteLocation, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return nil, err
	}
	var output []sdk.RemoteLocation
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		id, e := connector.resolveWarehouse(ctx, configuration, credential)
		if e != nil {
			return e
		}
		location := sdk.RemoteLocation{RemoteID: id, Name: configuration.Warehouse}
		if location.Validate() != nil {
			return ErrInvalidResponse
		}
		output = []sdk.RemoteLocation{location}
		return nil
	})
	return output, err
}

func (connector *Connector) ReadInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.InventoryQuery) ([]sdk.RemoteInventory, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil || query.Validate(200) != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return nil, err
	}
	var output []sdk.RemoteInventory
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		warehouseID, e := connector.resolveWarehouse(ctx, configuration, credential)
		if e != nil {
			return e
		}
		// The configured warehouse's id can only be known after a network
		// round trip, unlike Magento's compile-time constant, so this
		// request-shape check happens inside the credentialed closure
		// rather than before it.
		if query.LocationRemoteID != warehouseID {
			return sdk.ErrInvalidReadRequest
		}
		output = make([]sdk.RemoteInventory, 0, len(query.VariantRemoteIDs))
		for _, remoteID := range query.VariantRemoteIDs {
			detail, fetchErr := connector.fetchVariant(ctx, configuration, credential, remoteID)
			if fetchErr != nil {
				return fetchErr
			}
			quantity, ok := detail.stockIn(configuration.Warehouse)
			if !ok || quantity < 0 {
				return ErrInvalidResponse
			}
			row := sdk.RemoteInventory{LocationRemoteID: warehouseID, VariantRemoteID: remoteID, Quantity: int64(quantity)}
			if row.Validate() != nil {
				return ErrInvalidResponse
			}
			output = append(output, row)
		}
		return nil
	})
	return output, err
}

const setStockQuery = `mutation SetStock($variantId: ID!, $warehouse: ID!, $quantity: Int!) {
  productVariantStocksUpdate(variantId: $variantId, stocks: [{warehouse: $warehouse, quantity: $quantity}]) {
    productVariant { id }
    errors { field message code }
  }
}`

func (connector *Connector) WriteInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.InventoryWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		warehouseID, e := connector.resolveWarehouse(ctx, configuration, credential)
		if e != nil {
			return e
		}
		if request.LocationRemoteID != "" && request.LocationRemoteID != warehouseID {
			return sdk.ErrInvalidCommerceWrite
		}
		current, e := connector.fetchVariant(ctx, configuration, credential, request.VariantRemoteID)
		if e != nil {
			return e
		}
		if quantity, ok := current.stockIn(configuration.Warehouse); ok && int64(quantity) == request.Quantity {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		var payload struct {
			ProductVariantStocksUpdate struct {
				Errors []mutationErrorEntry `json:"errors"`
			} `json:"productVariantStocksUpdate"`
		}
		data, callErr := connector.graphql(ctx, configuration, credential, setStockQuery, map[string]any{"variantId": request.VariantRemoteID, "warehouse": warehouseID, "quantity": request.Quantity})
		if callErr == nil {
			if json.Unmarshal(data, &payload) != nil {
				callErr = ErrInvalidResponse
			} else {
				callErr = mutationErr(payload.ProductVariantStocksUpdate.Errors)
			}
		}
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			reconciled, reconcileErr := connector.fetchVariant(ctx, configuration, credential, request.VariantRemoteID)
			if reconcileErr == nil {
				if quantity, ok := reconciled.stockIn(configuration.Warehouse); ok && int64(quantity) == request.Quantity {
					receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
					return receipt.Validate()
				}
			}
			return writeOutcomeUnknown()
		}
		updated, e := connector.fetchVariant(ctx, configuration, credential, request.VariantRemoteID)
		quantity, ok := updated.stockIn(configuration.Warehouse)
		if e != nil || !ok || int64(quantity) != request.Quantity {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}
