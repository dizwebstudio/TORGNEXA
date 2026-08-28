package shopify

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
		response, callErr := connector.call(ctx, configuration, credential, "GET", "/locations.json", nil, nil)
		if callErr != nil {
			return callErr
		}
		var page struct {
			Locations []shopifyLocation `json:"locations"`
		}
		if json.Unmarshal(response.Body, &page) != nil || len(page.Locations) > 250 {
			return ErrInvalidResponse
		}
		output = make([]sdk.RemoteLocation, 0, len(page.Locations))
		for _, location := range page.Locations {
			if location.ID < 1 || !location.Active || !validRemoteText(location.Name, 200) {
				continue
			}
			item := sdk.RemoteLocation{RemoteID: intString(location.ID), Name: location.Name}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			output = append(output, item)
		}
		return nil
	})
	return output, err
}

// fetchInventoryItemID resolves a variant's distinct inventory_item_id: this
// is a different id than the variant's own id, and inventory_levels only
// accepts the former.
func (connector *Connector) fetchInventoryItemID(ctx context.Context, configuration Configuration, credential credentials, variantID int64) (int64, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/variants/"+intString(variantID)+".json", []QueryParam{{Name: "fields", Value: "id,inventory_item_id"}}, nil)
	if err != nil {
		return 0, err
	}
	var value struct {
		Variant struct {
			ID              int64 `json:"id"`
			InventoryItemID int64 `json:"inventory_item_id"`
		} `json:"variant"`
	}
	if json.Unmarshal(response.Body, &value) != nil || value.Variant.ID != variantID || value.Variant.InventoryItemID < 1 {
		return 0, ErrInvalidResponse
	}
	return value.Variant.InventoryItemID, nil
}

func (connector *Connector) fetchInventoryLevel(ctx context.Context, configuration Configuration, credential credentials, inventoryItemID, locationID int64) (int64, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/inventory_levels.json", []QueryParam{{Name: "inventory_item_ids", Value: intString(inventoryItemID)}, {Name: "location_ids", Value: intString(locationID)}}, nil)
	if err != nil {
		return 0, err
	}
	var page struct {
		InventoryLevels []shopifyInventoryLevel `json:"inventory_levels"`
	}
	if json.Unmarshal(response.Body, &page) != nil || len(page.InventoryLevels) != 1 || page.InventoryLevels[0].InventoryItemID != inventoryItemID || page.InventoryLevels[0].LocationID != locationID || page.InventoryLevels[0].Available < 0 {
		remote, _ := sdk.NewRemoteError(sdk.ErrorNotFound, "inventory_not_connected", "", 0)
		return 0, remote
	}
	return page.InventoryLevels[0].Available, nil
}

func (connector *Connector) ReadInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.InventoryQuery) ([]sdk.RemoteInventory, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil || query.Validate(200) != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	locationID, err := parsePositiveID(query.LocationRemoteID)
	if err != nil {
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
			variantID, e := parseVariantRemoteID(remoteID)
			if e != nil {
				return sdk.ErrInvalidReadRequest
			}
			inventoryItemID, e := connector.fetchInventoryItemID(ctx, configuration, credential, variantID)
			if e != nil {
				return e
			}
			quantity, e := connector.fetchInventoryLevel(ctx, configuration, credential, inventoryItemID, locationID)
			if e != nil {
				return e
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

// shopifyPrimaryLocation resolves the shop's default location: unlike
// WooCommerce, Shopify genuinely supports multiple locations, but
// sdk.InventoryWriteRequest (unlike the read-side InventoryQuery) carries no
// location field — it models a single-location write, so this picks the
// shop's own designated default rather than guessing among several.
func (connector *Connector) shopifyPrimaryLocation(ctx context.Context, configuration Configuration, credential credentials) (int64, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/shop.json", []QueryParam{{Name: "fields", Value: "primary_location_id"}}, nil)
	if err != nil {
		return 0, err
	}
	var value struct {
		Shop struct {
			PrimaryLocationID int64 `json:"primary_location_id"`
		} `json:"shop"`
	}
	if json.Unmarshal(response.Body, &value) != nil || value.Shop.PrimaryLocationID < 1 {
		return 0, ErrInvalidResponse
	}
	return value.Shop.PrimaryLocationID, nil
}

func (connector *Connector) WriteInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.InventoryWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	variantID, err := parseVariantRemoteID(request.VariantRemoteID)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		locationID, e := connector.shopifyPrimaryLocation(ctx, configuration, credential)
		if e != nil {
			return e
		}
		inventoryItemID, e := connector.fetchInventoryItemID(ctx, configuration, credential, variantID)
		if e != nil {
			return e
		}
		current, e := connector.fetchInventoryLevel(ctx, configuration, credential, inventoryItemID, locationID)
		if e == nil && current == request.Quantity {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]any{"inventory_item_id": inventoryItemID, "location_id": locationID, "available": request.Quantity})
		_, callErr := connector.call(ctx, configuration, credential, "POST", "/inventory_levels/set.json", nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			current, reconcileErr := connector.fetchInventoryLevel(ctx, configuration, credential, inventoryItemID, locationID)
			if reconcileErr == nil && current == request.Quantity {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		current, e = connector.fetchInventoryLevel(ctx, configuration, credential, inventoryItemID, locationID)
		if e != nil || current != request.Quantity {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}
