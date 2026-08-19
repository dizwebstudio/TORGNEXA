package ozon

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type warehouseListRequest struct {
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor,omitempty"`
}

type warehouseListResponse struct {
	Warehouses []struct {
		WarehouseID int64  `json:"warehouse_id"`
		Name        string `json:"name"`
	} `json:"warehouses"`
	Cursor string `json:"cursor"`
}

type stocksRequest struct {
	OfferIDs []string `json:"offer_id"`
	Limit    int      `json:"limit"`
	Cursor   string   `json:"cursor,omitempty"`
}

type stocksResponse struct {
	Items []struct {
		OfferID string `json:"offer_id"`
		SKU     int64  `json:"sku"`
		Stocks  []struct {
			WarehouseID int64 `json:"warehouse_id"`
			Present     int64 `json:"present"`
			Reserved    int64 `json:"reserved"`
		} `json:"stocks"`
	} `json:"items"`
	Cursor string `json:"cursor"`
}

func (connector *Connector) ListInventoryLocations(ctx context.Context, account sdk.Account, runtime sdk.Runtime) ([]sdk.RemoteLocation, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	// Task 012 keeps this SDK method bounded. If Ozon returns a continuation
	// cursor, the connector rejects the partial list instead of silently hiding
	// warehouses from reconciliation.
	body, _ := json.Marshal(warehouseListRequest{Limit: 1000})
	var result []sdk.RemoteLocation
	err := connector.withCredentials(ctx, runtime, account.SecretReference, func(clientID, apiKey []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/v2/warehouse/list", Body: body, ClientID: clientID, APIKey: apiKey})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		var parsed warehouseListResponse
		if len(response.Body) == 0 || json.Unmarshal(response.Body, &parsed) != nil || len(parsed.Warehouses) > 1000 || parsed.Cursor != "" {
			return ErrInvalidResponse
		}
		result = make([]sdk.RemoteLocation, 0, len(parsed.Warehouses))
		seen := make(map[int64]struct{}, len(parsed.Warehouses))
		for _, item := range parsed.Warehouses {
			location := sdk.RemoteLocation{RemoteID: strconv.FormatInt(item.WarehouseID, 10), Name: item.Name}
			if item.WarehouseID <= 0 || location.Validate() != nil {
				return ErrInvalidResponse
			}
			if _, duplicate := seen[item.WarehouseID]; duplicate {
				return ErrInvalidResponse
			}
			seen[item.WarehouseID] = struct{}{}
			result = append(result, location)
		}
		return nil
	})
	return result, err
}

func (connector *Connector) ReadInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.InventoryQuery) ([]sdk.RemoteInventory, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil || query.Validate(1000) != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	warehouseID, err := strconv.ParseInt(query.LocationRemoteID, 10, 64)
	if err != nil || warehouseID <= 0 {
		return nil, sdk.ErrInvalidReadRequest
	}
	request := stocksRequest{OfferIDs: append([]string(nil), query.VariantRemoteIDs...), Limit: 1000}
	body, _ := json.Marshal(request)
	var result []sdk.RemoteInventory
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(clientID, apiKey []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/v2/product/info/stocks-by-warehouse/fbs", Body: body, ClientID: clientID, APIKey: apiKey})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		var parsed stocksResponse
		if len(response.Body) == 0 || json.Unmarshal(response.Body, &parsed) != nil || len(parsed.Items) > len(query.VariantRemoteIDs) || parsed.Cursor != "" {
			return ErrInvalidResponse
		}
		allowed := make(map[string]struct{}, len(query.VariantRemoteIDs))
		for _, id := range query.VariantRemoteIDs {
			allowed[id] = struct{}{}
		}
		seenOffers := make(map[string]struct{}, len(parsed.Items))
		result = make([]sdk.RemoteInventory, 0, len(parsed.Items))
		for _, item := range parsed.Items {
			if _, ok := allowed[item.OfferID]; !ok || !validRemoteText(item.OfferID, 200) || item.SKU < 0 {
				return ErrInvalidResponse
			}
			if _, duplicate := seenOffers[item.OfferID]; duplicate {
				return ErrInvalidResponse
			}
			seenOffers[item.OfferID] = struct{}{}
			seenWarehouse := map[int64]struct{}{}
			matched := false
			for _, stock := range item.Stocks {
				if stock.WarehouseID <= 0 || stock.Present < 0 || stock.Reserved < 0 || stock.Reserved > stock.Present {
					return ErrInvalidResponse
				}
				if _, duplicate := seenWarehouse[stock.WarehouseID]; duplicate {
					return ErrInvalidResponse
				}
				seenWarehouse[stock.WarehouseID] = struct{}{}
				if stock.WarehouseID == warehouseID {
					matched = true
					available := stock.Present - stock.Reserved
					inventory := sdk.RemoteInventory{LocationRemoteID: query.LocationRemoteID, VariantRemoteID: item.OfferID, Quantity: available}
					if inventory.Validate() != nil {
						return ErrInvalidResponse
					}
					result = append(result, inventory)
				}
			}
			if !matched {
				// A requested offer may legitimately have no row on this
				// warehouse; represent that as zero instead of omission so
				// reconciliation can distinguish zero from partial response.
				inventory := sdk.RemoteInventory{LocationRemoteID: query.LocationRemoteID, VariantRemoteID: item.OfferID, Quantity: 0}
				if inventory.Validate() != nil {
					return ErrInvalidResponse
				}
				result = append(result, inventory)
			}
		}
		if len(parsed.Items) != len(query.VariantRemoteIDs) {
			return ErrInvalidResponse
		}
		return nil
	})
	return result, err
}
