package magnitmarket

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type shortSKUResponse struct {
	Result []struct {
		Barcode     int64  `json:"barcode"`
		ProductID   int64  `json:"product_id"`
		SellerSKUID string `json:"seller_sku_id"`
		SKUID       int64  `json:"sku_id"`
	} `json:"result"`
	ResultCount int   `json:"result_count"`
	ShopID      int64 `json:"shop_id"`
}

type priceInfoResponse struct {
	Result []struct {
		CurrencyCode string      `json:"currency_code"`
		OldPrice     json.Number `json:"old_price"`
		Price        json.Number `json:"price"`
		SellerSKUID  string      `json:"seller_sku_id"`
		SKUID        int64       `json:"sku_id"`
		Timestamp    string      `json:"timestamp"`
	} `json:"result"`
}

func (connector *Connector) ReadPrices(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.PricePage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.read") != nil || request.Validate(100) != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.PricePage{}, err
	}
	lastKey, err := parseLastKeyCursor(request.Cursor, configuration.fingerprint("prices"))
	if err != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	body, _ := json.Marshal(map[string]any{
		"filters":    map[string]any{},
		"pagination": map[string]any{"dir": "ASC", "last_key": lastKey, "limit": request.Limit},
	})
	var output sdk.PricePage
	err = connector.withAPIKey(ctx, runtime, account.SecretReference, func(key []byte) error {
		shortResponse, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/api/seller/v1/products/sku/shops/" + strconv.FormatInt(configuration.ShopID, 10) + "/short/list", Body: body, APIKey: key})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(shortResponse); remote != nil {
			return remote
		}
		skuIDs, parseErr := parseShortSKUs(shortResponse.Body, request.Limit, configuration.ShopID, lastKey)
		if parseErr != nil {
			return parseErr
		}
		if len(skuIDs) == 0 {
			output = sdk.PricePage{}
			return nil
		}
		priceBody, _ := json.Marshal(map[string]any{
			"filter":     map[string]any{"sku_ids": skuIDs},
			"pagination": map[string]any{"dir": "ASC", "page": 0, "page_size": len(skuIDs)},
		})
		response, priceCallErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/api/seller/v1/products/sku/price/info", Body: priceBody, APIKey: key})
		if priceCallErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		prices, parseErr := parsePrices(response.Body, skuIDs)
		if parseErr != nil {
			return parseErr
		}
		next := ""
		if len(skuIDs) == request.Limit {
			next, parseErr = makeLastKeyCursor(skuIDs[len(skuIDs)-1], configuration.fingerprint("prices"))
			if parseErr != nil {
				return parseErr
			}
		}
		output = sdk.PricePage{Items: prices, NextCursor: next}
		if output.Validate(request.Limit) != nil {
			return ErrInvalidResponse
		}
		return nil
	})
	return output, err
}

func parseShortSKUs(body []byte, limit int, shopID, previous int64) ([]int64, error) {
	var parsed shortSKUResponse
	if len(body) == 0 || len(body) > maxBodyBytes || decodeUseNumber(body, &parsed) != nil || parsed.ShopID != shopID || len(parsed.Result) > limit || parsed.ResultCount != len(parsed.Result) {
		return nil, ErrInvalidResponse
	}
	output := make([]int64, 0, len(parsed.Result))
	seen := map[int64]struct{}{}
	last := previous
	for _, item := range parsed.Result {
		if item.SKUID < 1 || item.SKUID <= last || item.ProductID < 1 || !validText(item.SellerSKUID, 80) || item.Barcode < 0 {
			return nil, ErrInvalidResponse
		}
		if _, duplicate := seen[item.SKUID]; duplicate {
			return nil, ErrInvalidResponse
		}
		seen[item.SKUID] = struct{}{}
		output = append(output, item.SKUID)
		last = item.SKUID
	}
	return output, nil
}

func parsePrices(body []byte, requested []int64) ([]sdk.RemotePrice, error) {
	var parsed priceInfoResponse
	if len(body) == 0 || len(body) > maxBodyBytes || decodeUseNumber(body, &parsed) != nil || len(parsed.Result) != len(requested) {
		return nil, ErrInvalidResponse
	}
	requestedSet := make(map[int64]struct{}, len(requested))
	for _, id := range requested {
		if id < 1 {
			return nil, ErrInvalidResponse
		}
		requestedSet[id] = struct{}{}
	}
	output := make([]sdk.RemotePrice, 0, len(parsed.Result))
	seen := map[int64]struct{}{}
	for _, item := range parsed.Result {
		if _, wanted := requestedSet[item.SKUID]; !wanted || item.SKUID < 1 || !validText(item.SellerSKUID, 80) || !strictPositiveMoney(item.Price.String()) || !validCurrency(item.CurrencyCode) {
			return nil, ErrInvalidResponse
		}
		if _, duplicate := seen[item.SKUID]; duplicate {
			return nil, ErrInvalidResponse
		}
		seen[item.SKUID] = struct{}{}
		updatedAt, err := parseUTC(item.Timestamp)
		if err != nil {
			return nil, ErrInvalidResponse
		}
		compareAt := ""
		if item.OldPrice.String() != "" && item.OldPrice.String() != "0" && item.OldPrice.String() != "0.0" {
			if !strictPositiveMoney(item.OldPrice.String()) {
				return nil, ErrInvalidResponse
			}
			compareAt = item.OldPrice.String()
		}
		price := sdk.RemotePrice{VariantRemoteID: strconv.FormatInt(item.SKUID, 10), Value: item.Price.String(), CompareAt: compareAt, Currency: item.CurrencyCode, UpdatedAt: updatedAt}
		if price.Validate() != nil {
			return nil, ErrInvalidResponse
		}
		output = append(output, price)
	}
	if len(seen) != len(requestedSet) {
		return nil, ErrInvalidResponse
	}
	return output, nil
}

func parsePriceTimestamps(body []byte, requested []int64) (map[int64]string, error) {
	var parsed priceInfoResponse
	if len(body) == 0 || len(body) > maxBodyBytes || decodeUseNumber(body, &parsed) != nil || len(parsed.Result) != len(requested) {
		return nil, ErrInvalidResponse
	}
	requestedSet := make(map[int64]struct{}, len(requested))
	for _, id := range requested {
		requestedSet[id] = struct{}{}
	}
	output := make(map[int64]string, len(requested))
	for _, item := range parsed.Result {
		if _, wanted := requestedSet[item.SKUID]; !wanted || item.SKUID < 1 || item.Timestamp == "" {
			return nil, ErrInvalidResponse
		}
		if _, duplicate := output[item.SKUID]; duplicate {
			return nil, ErrInvalidResponse
		}
		if _, err := parseUTC(item.Timestamp); err != nil {
			return nil, ErrInvalidResponse
		}
		output[item.SKUID] = item.Timestamp
	}
	if len(output) != len(requestedSet) {
		return nil, ErrInvalidResponse
	}
	return output, nil
}
