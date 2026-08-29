package shopware

import (
	"context"
	"encoding/json"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// storeLocationID is a synthetic single location: Shopware's core stock
// model (unlike Shopify/Medusa) is not natively multi-location, the same
// simplification WooCommerce/OpenCart already make.
const storeLocationID = "shopware-store"

func (connector *Connector) ListInventoryLocations(ctx context.Context, account sdk.Account, runtime sdk.Runtime) ([]sdk.RemoteLocation, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	if _, err := connector.configuration(ctx, account); err != nil {
		return nil, err
	}
	return []sdk.RemoteLocation{{RemoteID: storeLocationID, Name: "Shopware store stock"}}, nil
}

func (connector *Connector) fetchStock(ctx context.Context, configuration Configuration, accountID string, credential credentials, productID string) (int64, error) {
	response, err := connector.call(ctx, configuration, accountID, credential, "GET", "/product/"+productID, nil, nil)
	if err != nil {
		return 0, err
	}
	var value struct {
		Data shopwareProduct `json:"data"`
	}
	if json.Unmarshal(response.Body, &value) != nil || value.Data.ID != productID || value.Data.Stock < 0 {
		return 0, ErrInvalidResponse
	}
	return value.Data.Stock, nil
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
			quantity, e := connector.fetchStock(ctx, configuration, account.ID, credential, remoteID)
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
		current, e := connector.fetchStock(ctx, configuration, account.ID, credential, request.VariantRemoteID)
		if e == nil && current == request.Quantity {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]any{"stock": request.Quantity})
		_, callErr := connector.call(ctx, configuration, account.ID, credential, "PATCH", "/product/"+request.VariantRemoteID, nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			current, reconcileErr := connector.fetchStock(ctx, configuration, account.ID, credential, request.VariantRemoteID)
			if reconcileErr == nil && current == request.Quantity {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		current, e = connector.fetchStock(ctx, configuration, account.ID, credential, request.VariantRemoteID)
		if e != nil || current != request.Quantity {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}
