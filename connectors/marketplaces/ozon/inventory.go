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

// stockUpdateRequest is the exact single-row form accepted by Ozon's stock
// mutation. Ozon requires the parent product ID in addition to the seller
// offer ID, so the host must resolve both identities from typed mappings.
type stockUpdateRequest struct {
	Stocks []stockUpdateItem `json:"stocks"`
}

type stockUpdateItem struct {
	OfferID     string `json:"offer_id"`
	ProductID   int64  `json:"product_id"`
	Stock       int64  `json:"stock"`
	WarehouseID int64  `json:"warehouse_id"`
}

type stockUpdateResponse struct {
	Result []struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
		OfferID     string `json:"offer_id"`
		ProductID   int64  `json:"product_id"`
		Updated     bool   `json:"updated"`
		WarehouseID int64  `json:"warehouse_id"`
	} `json:"result"`
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

// WriteInventory updates one Ozon FBS warehouse stock. Both the seller offer
// and parent product identities are required because Ozon accepts the stock
// mutation as a tuple of offer_id, product_id and warehouse_id. A transport
// error is returned as an unknown remote outcome; the host must reconcile
// before attempting another write.
func (connector *Connector) WriteInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.InventoryWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	productID, err := strconv.ParseInt(request.ProductRemoteID, 10, 64)
	if err != nil || productID <= 0 {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	warehouseID, err := strconv.ParseInt(request.LocationRemoteID, 10, 64)
	if err != nil || warehouseID <= 0 || request.Quantity > 2_000_000_000 {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	item := stockUpdateItem{OfferID: request.VariantRemoteID, ProductID: productID, Stock: request.Quantity, WarehouseID: warehouseID}
	body, err := json.Marshal(stockUpdateRequest{Stocks: []stockUpdateItem{item}})
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(clientID, apiKey []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{
			Method:         "POST",
			Host:           apiHost,
			Path:           "/v2/products/stocks",
			Body:           body,
			ClientID:       clientID,
			APIKey:         apiKey,
			IdempotencyKey: request.IdempotencyKey,
		})
		if callErr != nil {
			return writeOutcomeUnknown()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		var parsed stockUpdateResponse
		if len(response.Body) == 0 || len(response.Body) > maxBodyBytes || json.Unmarshal(response.Body, &parsed) != nil || len(parsed.Result) != 1 {
			return ErrInvalidResponse
		}
		result := parsed.Result[0]
		if result.OfferID != item.OfferID || result.ProductID != item.ProductID || result.WarehouseID != item.WarehouseID || !validRemoteText(result.OfferID, 200) {
			return ErrInvalidResponse
		}
		if !result.Updated || len(result.Errors) != 0 {
			remote, remoteErr := sdk.NewRemoteError(sdk.ErrorInvalidRequest, "remote_rejected", "", 0)
			if remoteErr != nil {
				return ErrInvalidResponse
			}
			return remote
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}

var _ sdk.InventoryWriter = (*Connector)(nil)
