package yandexmarket

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const maxWarehousePages = 10

type warehousesResponse struct {
	Status string `json:"status"`
	Result *struct {
		Warehouses []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"warehouses"`
		Paging struct {
			NextPageToken string `json:"nextPageToken"`
		} `json:"paging"`
	} `json:"result"`
}

type stockDTO struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}
type stockOffer struct {
	OfferID   string     `json:"offerId"`
	Stocks    []stockDTO `json:"stocks"`
	UpdatedAt string     `json:"updatedAt"`
}
type partnerStocksResponse struct {
	Status string `json:"status"`
	Result *struct {
		PartnerWarehouseID int64        `json:"partnerWarehouseId"`
		Offers             []stockOffer `json:"offers"`
		Paging             struct {
			NextPageToken string `json:"nextPageToken"`
		} `json:"paging"`
	} `json:"result"`
}
type campaignStocksResponse struct {
	Status string `json:"status"`
	Result *struct {
		Warehouses []struct {
			WarehouseID int64        `json:"warehouseId"`
			Offers      []stockOffer `json:"offers"`
		} `json:"warehouses"`
		Paging struct {
			NextPageToken string `json:"nextPageToken"`
		} `json:"paging"`
	} `json:"result"`
}

func (connector *Connector) ListInventoryLocations(ctx context.Context, account sdk.Account, runtime sdk.Runtime) ([]sdk.RemoteLocation, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return nil, err
	}
	if configuration.InventoryMode == InventoryCampaignWarehouses {
		result := make([]sdk.RemoteLocation, 0, len(configuration.Warehouses))
		for _, warehouse := range configuration.Warehouses {
			item := sdk.RemoteLocation{RemoteID: strconv.FormatInt(warehouse.ID, 10), Name: warehouse.Name}
			if item.Validate() != nil {
				return nil, ErrInvalidConfiguration
			}
			result = append(result, item)
		}
		return result, nil
	}
	var output []sdk.RemoteLocation
	err = connector.withAPIKey(ctx, runtime, account.SecretReference, func(key []byte) error {
		seen := map[int64]struct{}{}
		token := ""
		for page := 0; page < maxWarehousePages; page++ {
			query := []QueryParam{{Name: "limit", Value: "30"}}
			if token != "" {
				query = append(query, QueryParam{Name: "pageToken", Value: token})
			}
			response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: businessV3Path(configuration.BusinessID, "/warehouses"), Query: query, Body: []byte(`{}`), APIKey: key})
			if callErr != nil {
				return normalizedTransportError()
			}
			if remote := normalizeHTTP(response); remote != nil {
				return remote
			}
			var parsed warehousesResponse
			if json.Unmarshal(response.Body, &parsed) != nil || parsed.Status != "OK" || parsed.Result == nil || len(parsed.Result.Warehouses) > 30 {
				return ErrInvalidResponse
			}
			for _, warehouse := range parsed.Result.Warehouses {
				if warehouse.ID < 1 || !validText(warehouse.Name, 300) {
					return ErrInvalidResponse
				}
				if _, duplicate := seen[warehouse.ID]; duplicate {
					return ErrInvalidResponse
				}
				seen[warehouse.ID] = struct{}{}
				item := sdk.RemoteLocation{RemoteID: strconv.FormatInt(warehouse.ID, 10), Name: warehouse.Name}
				if item.Validate() != nil {
					return ErrInvalidResponse
				}
				output = append(output, item)
			}
			next := parsed.Result.Paging.NextPageToken
			if next == "" {
				return nil
			}
			if next == token || !validTokenText(next) {
				return ErrInvalidResponse
			}
			token = next
		}
		return ErrInvalidResponse
	})
	return output, err
}

func (connector *Connector) ReadInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.InventoryQuery) ([]sdk.RemoteInventory, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil || query.Validate(500) != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return nil, err
	}
	warehouseID, err := strconv.ParseInt(query.LocationRemoteID, 10, 64)
	if err != nil || warehouseID < 1 || !configuredWarehouseAllowed(configuration, warehouseID) {
		return nil, sdk.ErrInvalidReadRequest
	}
	body, _ := json.Marshal(map[string]any{"offerIds": query.VariantRemoteIDs, "partnerWarehouseId": warehouseID})
	path := businessV3Path(configuration.BusinessID, "/offers/stocks")
	if configuration.InventoryMode == InventoryCampaignWarehouses {
		body, _ = json.Marshal(map[string]any{"offerIds": query.VariantRemoteIDs, "stocksWarehouseId": warehouseID, "withTurnover": false})
		path = campaignPath(configuration.CampaignID, "/offers/stocks")
	}
	var output []sdk.RemoteInventory
	err = connector.withAPIKey(ctx, runtime, account.SecretReference, func(key []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: path, Body: body, APIKey: key})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		var offers []stockOffer
		if configuration.InventoryMode == InventoryPartnerWarehouses {
			var parsed partnerStocksResponse
			if json.Unmarshal(response.Body, &parsed) != nil || parsed.Status != "OK" || parsed.Result == nil || parsed.Result.PartnerWarehouseID != warehouseID || parsed.Result.Paging.NextPageToken != "" {
				return ErrInvalidResponse
			}
			offers = parsed.Result.Offers
		} else {
			var parsed campaignStocksResponse
			if json.Unmarshal(response.Body, &parsed) != nil || parsed.Status != "OK" || parsed.Result == nil || parsed.Result.Paging.NextPageToken != "" || len(parsed.Result.Warehouses) != 1 || parsed.Result.Warehouses[0].WarehouseID != warehouseID {
				return ErrInvalidResponse
			}
			offers = parsed.Result.Warehouses[0].Offers
		}
		items, parseErr := normalizeStockOffers(query, offers)
		if parseErr != nil {
			return parseErr
		}
		output = items
		return nil
	})
	return output, err
}

func configuredWarehouseAllowed(configuration Configuration, id int64) bool {
	if configuration.InventoryMode == InventoryPartnerWarehouses {
		return true
	}
	for _, warehouse := range configuration.Warehouses {
		if warehouse.ID == id {
			return true
		}
	}
	return false
}

func normalizeStockOffers(query sdk.InventoryQuery, offers []stockOffer) ([]sdk.RemoteInventory, error) {
	if len(offers) != len(query.VariantRemoteIDs) {
		return nil, ErrInvalidResponse
	}
	allowed := make(map[string]struct{}, len(query.VariantRemoteIDs))
	for _, id := range query.VariantRemoteIDs {
		allowed[id] = struct{}{}
	}
	seenOffers := map[string]struct{}{}
	result := make([]sdk.RemoteInventory, 0, len(offers))
	for _, offer := range offers {
		if !validText(offer.OfferID, 255) || len(offer.Stocks) > 16 {
			return nil, ErrInvalidResponse
		}
		if _, ok := allowed[offer.OfferID]; !ok {
			return nil, ErrInvalidResponse
		}
		if _, duplicate := seenOffers[offer.OfferID]; duplicate {
			return nil, ErrInvalidResponse
		}
		seenOffers[offer.OfferID] = struct{}{}
		seenTypes := map[string]struct{}{}
		var available int64
		for _, stock := range offer.Stocks {
			if !validText(stock.Type, 32) || stock.Count < 0 {
				return nil, ErrInvalidResponse
			}
			if _, duplicate := seenTypes[stock.Type]; duplicate {
				return nil, ErrInvalidResponse
			}
			seenTypes[stock.Type] = struct{}{}
			if stock.Type == "AVAILABLE" {
				available = stock.Count
			}
		}
		item := sdk.RemoteInventory{LocationRemoteID: query.LocationRemoteID, VariantRemoteID: offer.OfferID, Quantity: available}
		if item.Validate() != nil {
			return nil, ErrInvalidResponse
		}
		result = append(result, item)
	}
	return result, nil
}
