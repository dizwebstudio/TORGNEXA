package woocommerce

import (
	"context"
	"encoding/json"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const storeLocationID = "woocommerce-store"

func (connector *Connector) ListInventoryLocations(ctx context.Context, account sdk.Account, runtime sdk.Runtime) ([]sdk.RemoteLocation, error) {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	if _, err := connector.configuration(ctx, account); err != nil {
		return nil, err
	}
	return []sdk.RemoteLocation{{RemoteID: storeLocationID, Name: "WooCommerce store stock"}}, nil
}

func (connector *Connector) fetchStock(ctx context.Context, configuration Configuration, credential credentials, target variantTarget) (int64, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", target.path(), nil, nil)
	if err != nil {
		return 0, err
	}
	var value struct {
		ManageStock   any    `json:"manage_stock"`
		StockQuantity *int64 `json:"stock_quantity"`
		StockStatus   string `json:"stock_status"`
	}
	if json.Unmarshal(response.Body, &value) != nil || value.StockQuantity == nil || *value.StockQuantity < 0 {
		remote, _ := sdk.NewRemoteError(sdk.ErrorUnsupported, "managed_stock_required", "", 0)
		return 0, remote
	}
	return *value.StockQuantity, nil
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
			target, e := parseVariantTarget(remoteID)
			if e != nil {
				return sdk.ErrInvalidReadRequest
			}
			quantity, e := connector.fetchStock(ctx, configuration, credential, target)
			if e != nil {
				return e
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
