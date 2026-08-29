package magento

import (
	"context"
	"encoding/json"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// storeLocationID is a synthetic single location: this connector uses
// Magento's classic single/default-source CatalogInventory stock API, not
// the multi-source inventory (MSI) module, the same single-location
// simplification WooCommerce/OpenCart/Shopware already make.
const storeLocationID = "magento-store"

func (connector *Connector) ListInventoryLocations(ctx context.Context, account sdk.Account, runtime sdk.Runtime) ([]sdk.RemoteLocation, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	if _, err := connector.configuration(ctx, account); err != nil {
		return nil, err
	}
	return []sdk.RemoteLocation{{RemoteID: storeLocationID, Name: "Magento store stock"}}, nil
}

func (connector *Connector) fetchStockItem(ctx context.Context, configuration Configuration, credential credentials, sku string) (magentoStockItem, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/stockItems/"+pathSKU(sku), nil, nil)
	if err != nil {
		return magentoStockItem{}, err
	}
	var item magentoStockItem
	if json.Unmarshal(response.Body, &item) != nil {
		return magentoStockItem{}, ErrInvalidResponse
	}
	return item, nil
}

func (connector *Connector) ReadInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.InventoryQuery) ([]sdk.RemoteInventory, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil || query.Validate(200) != nil || query.LocationRemoteID != storeLocationID {
		return nil, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return nil, err
	}
	var output []sdk.RemoteInventory
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		output = make([]sdk.RemoteInventory, 0, len(query.VariantRemoteIDs))
		for _, remoteID := range query.VariantRemoteIDs {
			item, e := connector.fetchStockItem(ctx, configuration, credential, remoteID)
			if e != nil {
				return e
			}
			quantity, qErr := item.Qty.Int64()
			if qErr != nil || quantity < 0 {
				return ErrInvalidResponse
			}
			row := sdk.RemoteInventory{LocationRemoteID: storeLocationID, VariantRemoteID: remoteID, Quantity: quantity}
			if row.Validate() != nil {
				return ErrInvalidResponse
			}
			output = append(output, row)
		}
		return nil
	})
	return output, err
}

func (connector *Connector) WriteInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.InventoryWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	if request.LocationRemoteID != "" && request.LocationRemoteID != storeLocationID {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		current, e := connector.fetchStockItem(ctx, configuration, credential, request.VariantRemoteID)
		if e != nil {
			return e
		}
		currentQty, qErr := current.Qty.Int64()
		if qErr == nil && currentQty == request.Quantity {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		itemID := current.ItemID.String()
		if itemID == "" {
			return ErrInvalidResponse
		}
		body, _ := json.Marshal(map[string]any{"stockItem": map[string]any{"qty": request.Quantity, "is_in_stock": request.Quantity > 0}})
		_, callErr := connector.call(ctx, configuration, credential, "PUT", "/products/"+pathSKU(request.VariantRemoteID)+"/stockItems/"+itemID, nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			reconciled, reconcileErr := connector.fetchStockItem(ctx, configuration, credential, request.VariantRemoteID)
			reconciledQty, reconcileQErr := reconciled.Qty.Int64()
			if reconcileErr == nil && reconcileQErr == nil && reconciledQty == request.Quantity {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		updated, e := connector.fetchStockItem(ctx, configuration, credential, request.VariantRemoteID)
		updatedQty, qErr := updated.Qty.Int64()
		if e != nil || qErr != nil || updatedQty != request.Quantity {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}
