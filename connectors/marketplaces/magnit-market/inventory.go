package magnitmarket

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type stockInfoResponse struct {
	Result []struct {
		SellerSKUID string `json:"seller_sku_id"`
		SKUID       int64  `json:"sku_id"`
		Details     []struct {
			Reserved int64  `json:"reserved"`
			Stock    int64  `json:"stock"`
			Type     string `json:"type"`
		} `json:"stock_info_details"`
	} `json:"result"`
}

func (connector *Connector) ListInventoryLocations(ctx context.Context, account sdk.Account, runtime sdk.Runtime) ([]sdk.RemoteLocation, error) {
	if connector == nil || runtime == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return nil, err
	}
	location := sdk.RemoteLocation{RemoteID: configuration.inventoryLocationID(), Name: "Magnit Market " + string(configuration.StockType) + " aggregate"}
	if location.Validate() != nil {
		return nil, ErrInvalidConfiguration
	}
	return []sdk.RemoteLocation{location}, nil
}

func (connector *Connector) ReadInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.InventoryQuery) ([]sdk.RemoteInventory, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil || query.Validate(100) != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return nil, err
	}
	if query.LocationRemoteID != configuration.inventoryLocationID() {
		return nil, sdk.ErrInvalidReadRequest
	}
	skuIDs := make([]int64, 0, len(query.VariantRemoteIDs))
	requested := make(map[int64]string, len(query.VariantRemoteIDs))
	for _, remoteID := range query.VariantRemoteIDs {
		id, parseErr := strconv.ParseInt(remoteID, 10, 64)
		if parseErr != nil || id < 1 {
			return nil, sdk.ErrInvalidReadRequest
		}
		if _, duplicate := requested[id]; duplicate {
			return nil, sdk.ErrInvalidReadRequest
		}
		requested[id] = remoteID
		skuIDs = append(skuIDs, id)
	}
	body, _ := json.Marshal(map[string]any{
		"filter":     map[string]any{"sku_ids": skuIDs},
		"pagination": map[string]any{"dir": "ASC", "page": 0, "page_size": len(skuIDs)},
	})
	var output []sdk.RemoteInventory
	err = connector.withAPIKey(ctx, runtime, account.SecretReference, func(key []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/api/seller/v1/products/sku/stocks/info", Body: body, APIKey: key})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		values, parseErr := parseInventory(response.Body, requested, configuration)
		if parseErr != nil {
			return parseErr
		}
		output = values
		return nil
	})
	return output, err
}

func parseInventory(body []byte, requested map[int64]string, configuration Configuration) ([]sdk.RemoteInventory, error) {
	var parsed stockInfoResponse
	if len(body) == 0 || len(body) > maxBodyBytes || decodeUseNumber(body, &parsed) != nil || len(parsed.Result) != len(requested) {
		return nil, ErrInvalidResponse
	}
	output := make([]sdk.RemoteInventory, 0, len(parsed.Result))
	seen := map[int64]struct{}{}
	for _, item := range parsed.Result {
		remoteID, wanted := requested[item.SKUID]
		if !wanted || item.SKUID < 1 || !validText(item.SellerSKUID, 80) || len(item.Details) > 32 {
			return nil, ErrInvalidResponse
		}
		if _, duplicate := seen[item.SKUID]; duplicate {
			return nil, ErrInvalidResponse
		}
		seen[item.SKUID] = struct{}{}
		available := int64(0)
		matched := false
		for _, detail := range item.Details {
			if !validText(detail.Type, 32) || detail.Stock < 0 || detail.Reserved < 0 || detail.Reserved > detail.Stock {
				return nil, ErrInvalidResponse
			}
			if detail.Type == string(configuration.StockType) {
				if matched {
					return nil, ErrInvalidResponse
				}
				matched = true
				available = detail.Stock - detail.Reserved
			}
		}
		value := sdk.RemoteInventory{LocationRemoteID: configuration.inventoryLocationID(), VariantRemoteID: remoteID, Quantity: available}
		if value.Validate() != nil {
			return nil, ErrInvalidResponse
		}
		output = append(output, value)
	}
	if len(seen) != len(requested) {
		return nil, ErrInvalidResponse
	}
	return output, nil
}
