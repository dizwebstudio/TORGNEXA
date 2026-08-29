package bitrix

import (
	"context"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	bitrixInventoryPageSize = 50
	bitrixInventoryMaxRows  = 200
)

func (connector *Connector) listStores(ctx context.Context, configuration Configuration, credential credentials, offset int) ([]bitrixStore, int, error) {
	response, err := connector.call(ctx, configuration, credential, "catalog.store.list", map[string]any{
		"select": []string{"id", "title", "active"},
		"filter": map[string]any{"active": "Y"},
		"order":  map[string]string{"id": "asc"},
		"start":  offset,
	})
	if err != nil {
		return nil, 0, err
	}
	return decodeStoreList(response.Body)
}

func (connector *Connector) listStoreProducts(ctx context.Context, configuration Configuration, credential credentials, storeID int64, productIDs []int64, offset int) ([]bitrixStoreProduct, int, error) {
	response, err := connector.call(ctx, configuration, credential, "catalog.storeproduct.list", map[string]any{
		"select": []string{"id", "productId", "storeId", "amount"},
		"filter": map[string]any{"storeId": storeID, "@productId": productIDs},
		"order":  map[string]string{"id": "asc"},
		"start":  offset,
	})
	if err != nil {
		return nil, 0, err
	}
	return decodeStoreProductList(response.Body)
}

// ListInventoryLocations returns active Bitrix warehouses. The REST method is
// paged at 50 records, so the adapter drains a bounded result set before
// returning it through the non-paged SDK port.
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
		output = make([]sdk.RemoteLocation, 0, 8)
		for offset := 0; offset <= bitrixInventoryMaxRows; offset += bitrixInventoryPageSize {
			stores, total, listErr := connector.listStores(ctx, configuration, credential, offset)
			if listErr != nil {
				return listErr
			}
			if total > bitrixInventoryMaxRows {
				return ErrInvalidResponse
			}
			for _, store := range stores {
				if store.ID < 1 || store.Active != "Y" || !validRemoteText(store.Title, 300) {
					return ErrInvalidResponse
				}
				location := sdk.RemoteLocation{RemoteID: strconv.FormatInt(store.ID, 10), Name: store.Title}
				if location.Validate() != nil {
					return ErrInvalidResponse
				}
				output = append(output, location)
			}
			if len(output) >= total || len(stores) < bitrixInventoryPageSize {
				break
			}
		}
		if len(output) > bitrixInventoryMaxRows {
			return ErrInvalidResponse
		}
		return nil
	})
	return output, err
}

// ReadInventory reads integer stock balances for one Bitrix warehouse. The
// canonical SDK v1 quantity is int64; fractional amounts are rejected rather
// than rounded or converted through a binary float.
func (connector *Connector) ReadInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.InventoryQuery) ([]sdk.RemoteInventory, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil || query.Validate(bitrixInventoryMaxRows) != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	storeID, err := strconv.ParseInt(query.LocationRemoteID, 10, 64)
	if err != nil || storeID < 1 {
		return nil, sdk.ErrInvalidReadRequest
	}
	productIDs := make([]int64, 0, len(query.VariantRemoteIDs))
	for _, remoteID := range query.VariantRemoteIDs {
		productID, parseErr := strconv.ParseInt(remoteID, 10, 64)
		if parseErr != nil || productID < 1 {
			return nil, sdk.ErrInvalidReadRequest
		}
		productIDs = append(productIDs, productID)
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return nil, err
	}
	var output []sdk.RemoteInventory
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		rows := make(map[int64]bitrixStoreProduct, len(productIDs))
		for offset := 0; offset <= bitrixInventoryMaxRows; offset += bitrixInventoryPageSize {
			page, total, listErr := connector.listStoreProducts(ctx, configuration, credential, storeID, productIDs, offset)
			if listErr != nil {
				return listErr
			}
			if total > bitrixInventoryMaxRows || len(rows)+len(page) > bitrixInventoryMaxRows {
				return ErrInvalidResponse
			}
			for _, row := range page {
				if row.ID < 1 || row.ProductID < 1 || row.StoreID != storeID || row.Amount.String() == "" {
					return ErrInvalidResponse
				}
				if _, requested := rows[row.ProductID]; !requested && !containsInt64(productIDs, row.ProductID) {
					return ErrInvalidResponse
				}
				if _, duplicate := rows[row.ProductID]; duplicate {
					return ErrInvalidResponse
				}
				quantity, quantityErr := row.Amount.Int64()
				if quantityErr != nil || quantity < 0 {
					return ErrInvalidResponse
				}
				rows[row.ProductID] = row
			}
			if len(page) == 0 || len(page) < bitrixInventoryPageSize || len(rows) >= total {
				break
			}
		}
		output = make([]sdk.RemoteInventory, 0, len(query.VariantRemoteIDs))
		for _, remoteID := range query.VariantRemoteIDs {
			productID, _ := strconv.ParseInt(remoteID, 10, 64)
			quantity := int64(0)
			if row, ok := rows[productID]; ok {
				quantity, err = row.Amount.Int64()
				if err != nil {
					return ErrInvalidResponse
				}
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

func containsInt64(values []int64, target int64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
