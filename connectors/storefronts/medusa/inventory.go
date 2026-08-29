package medusa

import (
	"context"
	"encoding/json"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

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
		response, callErr := connector.call(ctx, configuration, credential, "GET", "/stock-locations", []QueryParam{{Name: "limit", Value: "100"}, {Name: "fields", Value: "id,name"}}, nil)
		if callErr != nil {
			return callErr
		}
		var page struct {
			StockLocations []medusaStockLocation `json:"stock_locations"`
		}
		if json.Unmarshal(response.Body, &page) != nil || len(page.StockLocations) > 100 {
			return ErrInvalidResponse
		}
		output = make([]sdk.RemoteLocation, 0, len(page.StockLocations))
		for _, location := range page.StockLocations {
			if location.ID == "" || !validRemoteText(location.Name, 200) {
				continue
			}
			item := sdk.RemoteLocation{RemoteID: location.ID, Name: location.Name}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			output = append(output, item)
		}
		return nil
	})
	return output, err
}

func (connector *Connector) fetchVariantSKU(ctx context.Context, configuration Configuration, credential credentials, productID, variantID string) (string, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/products/"+productID+"/variants/"+variantID, []QueryParam{{Name: "fields", Value: "id,sku"}}, nil)
	if err != nil {
		return "", err
	}
	var value struct {
		Variant medusaVariant `json:"variant"`
	}
	if json.Unmarshal(response.Body, &value) != nil || value.Variant.ID != variantID || value.Variant.SKU == "" {
		return "", ErrInvalidResponse
	}
	return value.Variant.SKU, nil
}

func (connector *Connector) fetchInventoryItemBySKU(ctx context.Context, configuration Configuration, credential credentials, sku string) (medusaInventoryItem, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/inventory-items", []QueryParam{{Name: "sku", Value: sku}, {Name: "limit", Value: "1"}, {Name: "fields", Value: "id,sku,location_levels.location_id,location_levels.stocked_quantity,location_levels.available_quantity"}}, nil)
	if err != nil {
		return medusaInventoryItem{}, err
	}
	var page struct {
		InventoryItems []medusaInventoryItem `json:"inventory_items"`
	}
	if json.Unmarshal(response.Body, &page) != nil || len(page.InventoryItems) > 1 {
		return medusaInventoryItem{}, ErrInvalidResponse
	}
	if len(page.InventoryItems) == 0 || page.InventoryItems[0].ID == "" {
		remote, _ := sdk.NewRemoteError(sdk.ErrorNotFound, "inventory_item_missing", "", 0)
		return medusaInventoryItem{}, remote
	}
	return page.InventoryItems[0], nil
}

func inventoryLevelFor(item medusaInventoryItem, locationID string) (int64, bool) {
	for _, level := range item.LocationLevels {
		if level.LocationID != locationID {
			continue
		}
		quantity, err := level.AvailableQuantity.Int64()
		if err != nil {
			return 0, false
		}
		return quantity, true
	}
	return 0, false
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
		output = make([]sdk.RemoteInventory, 0, len(query.VariantRemoteIDs))
		for _, remoteID := range query.VariantRemoteIDs {
			productID, variantID, e := parseVariantRemoteID(remoteID)
			if e != nil {
				return sdk.ErrInvalidReadRequest
			}
			sku, e := connector.fetchVariantSKU(ctx, configuration, credential, productID, variantID)
			if e != nil {
				return e
			}
			inventoryItem, e := connector.fetchInventoryItemBySKU(ctx, configuration, credential, sku)
			if e != nil {
				return e
			}
			quantity, found := inventoryLevelFor(inventoryItem, query.LocationRemoteID)
			if !found {
				remote, _ := sdk.NewRemoteError(sdk.ErrorNotFound, "inventory_level_missing", "", 0)
				return remote
			}
			item := sdk.RemoteInventory{LocationRemoteID: query.LocationRemoteID, VariantRemoteID: remoteID, Quantity: quantity}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			output = append(output, item)
		}
		return nil
	})
	return output, err
}

// medusaPrimaryLocation picks the first stock location returned, the same
// deterministic single-location choice Shopify's connector makes for writes:
// sdk.InventoryWriteRequest (unlike the read-side InventoryQuery) carries no
// location field, and Medusa has no location "is primary" flag to prefer.
func (connector *Connector) medusaPrimaryLocation(ctx context.Context, configuration Configuration, credential credentials) (string, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/stock-locations", []QueryParam{{Name: "limit", Value: "1"}, {Name: "fields", Value: "id"}}, nil)
	if err != nil {
		return "", err
	}
	var page struct {
		StockLocations []medusaStockLocation `json:"stock_locations"`
	}
	if json.Unmarshal(response.Body, &page) != nil || len(page.StockLocations) == 0 || page.StockLocations[0].ID == "" {
		return "", ErrInvalidResponse
	}
	return page.StockLocations[0].ID, nil
}

func (connector *Connector) WriteInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.InventoryWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	productID, variantID, err := parseVariantRemoteID(request.VariantRemoteID)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		locationID, e := connector.medusaPrimaryLocation(ctx, configuration, credential)
		if e != nil {
			return e
		}
		if request.LocationRemoteID != "" && request.LocationRemoteID != locationID {
			return sdk.ErrInvalidCommerceWrite
		}
		sku, e := connector.fetchVariantSKU(ctx, configuration, credential, productID, variantID)
		if e != nil {
			return e
		}
		inventoryItem, e := connector.fetchInventoryItemBySKU(ctx, configuration, credential, sku)
		if e != nil {
			return e
		}
		if current, found := inventoryLevelFor(inventoryItem, locationID); found && current == request.Quantity {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]any{"stocked_quantity": request.Quantity})
		_, callErr := connector.call(ctx, configuration, credential, "POST", "/inventory-items/"+inventoryItem.ID+"/location-levels/"+locationID, nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			reconciled, reconcileErr := connector.fetchInventoryItemBySKU(ctx, configuration, credential, sku)
			if current, found := inventoryLevelFor(reconciled, locationID); reconcileErr == nil && found && current == request.Quantity {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		updated, e := connector.fetchInventoryItemBySKU(ctx, configuration, credential, sku)
		current, found := inventoryLevelFor(updated, locationID)
		if e != nil || !found || current != request.Quantity {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}
