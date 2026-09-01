package wildberries

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type warehousesResponse []struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type stocksRequest struct {
	ChrtIDs []int64 `json:"chrtIds"`
}

type stocksResponse struct {
	Stocks []struct {
		ChrtID int64 `json:"chrtId"`
		Amount int64 `json:"amount"`
	} `json:"stocks"`
}

// inventoryWriteRequest is the single-item form accepted by WB's FBS stock
// endpoint. chrtId is the size/variant identifier returned by the cards and
// stock APIs; it must not be confused with the product nmID.
type inventoryWriteRequest struct {
	Stocks []struct {
		ChrtID int64 `json:"chrtId"`
		Amount int64 `json:"amount"`
	} `json:"stocks"`
}

func (connector *Connector) ListInventoryLocations(ctx context.Context, account sdk.Account, runtime sdk.Runtime) ([]sdk.RemoteLocation, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	var result []sdk.RemoteLocation
	err := runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "GET", Host: marketplaceHost, Path: "/api/v3/warehouses", Token: secret})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		var parsed warehousesResponse
		if json.Unmarshal(response.Body, &parsed) != nil || len(parsed) > 10000 {
			return ErrInvalidResponse
		}
		result = make([]sdk.RemoteLocation, 0, len(parsed))
		seen := make(map[int64]struct{}, len(parsed))
		for _, item := range parsed {
			location := sdk.RemoteLocation{RemoteID: strconv.FormatInt(item.ID, 10), Name: item.Name}
			if item.ID <= 0 || location.Validate() != nil {
				return ErrInvalidResponse
			}
			if _, duplicate := seen[item.ID]; duplicate {
				return ErrInvalidResponse
			}
			seen[item.ID] = struct{}{}
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
	ids := make([]int64, 0, len(query.VariantRemoteIDs))
	for _, raw := range query.VariantRemoteIDs {
		id, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || id <= 0 {
			return nil, sdk.ErrInvalidReadRequest
		}
		ids = append(ids, id)
	}
	body, _ := json.Marshal(stocksRequest{ChrtIDs: ids})
	var result []sdk.RemoteInventory
	err = runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: marketplaceHost, Path: "/api/v3/stocks/" + strconv.FormatInt(warehouseID, 10), Body: body, Token: secret})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		var parsed stocksResponse
		if json.Unmarshal(response.Body, &parsed) != nil || len(parsed.Stocks) > len(ids) {
			return ErrInvalidResponse
		}
		allowed := make(map[int64]struct{}, len(ids))
		for _, id := range ids {
			allowed[id] = struct{}{}
		}
		seen := make(map[int64]struct{}, len(parsed.Stocks))
		result = make([]sdk.RemoteInventory, 0, len(parsed.Stocks))
		for _, stock := range parsed.Stocks {
			if stock.ChrtID <= 0 || stock.Amount < 0 {
				return ErrInvalidResponse
			}
			if _, ok := allowed[stock.ChrtID]; !ok {
				return ErrInvalidResponse
			}
			if _, duplicate := seen[stock.ChrtID]; duplicate {
				return ErrInvalidResponse
			}
			seen[stock.ChrtID] = struct{}{}
			item := sdk.RemoteInventory{LocationRemoteID: query.LocationRemoteID, VariantRemoteID: strconv.FormatInt(stock.ChrtID, 10), Quantity: stock.Amount}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			result = append(result, item)
		}
		return nil
	})
	return result, err
}

// WriteInventory updates one WB FBS warehouse stock. WB acknowledges this
// mutation without returning the resulting row, so the receipt is applied but
// not reconciled; the host must perform a subsequent inventory read. A
// transport failure is deliberately reported as an unknown remote outcome.
func (connector *Connector) WriteInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.InventoryWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	warehouseID, err := strconv.ParseInt(request.LocationRemoteID, 10, 64)
	if err != nil || warehouseID <= 0 || request.Quantity > 2_000_000_000 {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	chrtID, err := strconv.ParseInt(request.VariantRemoteID, 10, 64)
	if err != nil || chrtID <= 0 {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	body, err := json.Marshal(inventoryWriteRequest{Stocks: []struct {
		ChrtID int64 `json:"chrtId"`
		Amount int64 `json:"amount"`
	}{{ChrtID: chrtID, Amount: request.Quantity}}})
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	var receipt sdk.CommerceWriteReceipt
	err = runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{
			Method:         "PUT",
			Host:           marketplaceHost,
			Path:           "/api/v3/stocks/" + strconv.FormatInt(warehouseID, 10),
			Body:           body,
			Token:          secret,
			IdempotencyKey: request.IdempotencyKey,
		})
		if callErr != nil {
			return writeOutcomeUnknown()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}

var _ sdk.InventoryWriter = (*Connector)(nil)
