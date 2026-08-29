package magnitmarket

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type productListResponse struct {
	Result []struct {
		Barcode     int64  `json:"barcode"`
		ProductID   int64  `json:"product_id"`
		SellerSKUID string `json:"seller_sku_id"`
		SKUID       int64  `json:"sku_id"`
		Title       string `json:"title"`
		IsActive    bool   `json:"is_active"`
		IsArchive   bool   `json:"is_archive"`
		IsBlocked   bool   `json:"is_blocked"`
	} `json:"result"`
}

type productDTO struct {
	Barcode     int64
	ProductID   int64
	SellerSKUID string
	SKUID       int64
	Title       string
	Archived    bool
}

func (connector *Connector) ReadProducts(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ProductPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || request.Validate(100) != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ProductPage{}, err
	}
	pageNumber, err := parsePageCursor(request.Cursor, configuration.fingerprint("products"))
	if err != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	body, _ := json.Marshal(map[string]any{
		"filter":     map[string]any{"shop_id": configuration.ShopID},
		"pagination": map[string]any{"dir": "ASC", "page": pageNumber, "page_size": request.Limit},
	})
	var output sdk.ProductPage
	err = connector.withAPIKey(ctx, runtime, account.SecretReference, func(key []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/api/seller/v1/products/sku/list", Body: body, APIKey: key})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		products, parseErr := parseProductDTOs(response.Body, request.Limit)
		if parseErr != nil {
			return parseErr
		}
		skuIDs := make([]int64, 0, len(products))
		for _, product := range products {
			skuIDs = append(skuIDs, product.SKUID)
		}
		timestamps := map[int64]string{}
		if len(skuIDs) > 0 {
			priceBody, _ := json.Marshal(map[string]any{
				"filter":     map[string]any{"sku_ids": skuIDs},
				"pagination": map[string]any{"dir": "ASC", "page": 0, "page_size": len(skuIDs)},
			})
			priceResponse, priceCallErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/api/seller/v1/products/sku/price/info", Body: priceBody, APIKey: key})
			if priceCallErr != nil {
				return normalizedTransportError()
			}
			if remote := normalizeHTTP(priceResponse); remote != nil {
				return remote
			}
			timestamps, parseErr = parsePriceTimestamps(priceResponse.Body, skuIDs)
			if parseErr != nil {
				return parseErr
			}
		}
		result := make([]sdk.RemoteProduct, 0, len(products))
		for _, product := range products {
			updatedAt, parseErr := parseUTC(timestamps[product.SKUID])
			if parseErr != nil {
				return ErrInvalidResponse
			}
			skuID := strconv.FormatInt(product.SKUID, 10)
			aliases := []string{product.SellerSKUID}
			if product.Barcode > 0 {
				aliases = append(aliases, strconv.FormatInt(product.Barcode, 10))
			}
			remoteProduct := sdk.RemoteProduct{
				RemoteID:  strconv.FormatInt(product.ProductID, 10) + ":" + skuID,
				SellerSKU: product.SellerSKUID,
				Title:     product.Title,
				UpdatedAt: updatedAt,
				Variants:  []sdk.RemoteVariant{{RemoteID: skuID, SKUs: aliases}},
			}
			if remoteProduct.Validate() != nil {
				return ErrInvalidResponse
			}
			result = append(result, remoteProduct)
		}
		next := ""
		if len(products) == request.Limit {
			next, parseErr = makePageCursor(pageNumber+1, configuration.fingerprint("products"))
			if parseErr != nil {
				return parseErr
			}
		}
		output = sdk.ProductPage{Items: result, NextCursor: next}
		if output.Validate(request.Limit) != nil {
			return ErrInvalidResponse
		}
		return nil
	})
	return output, err
}

func parseProductDTOs(body []byte, limit int) ([]productDTO, error) {
	var parsed productListResponse
	if len(body) == 0 || len(body) > maxBodyBytes || decodeUseNumber(body, &parsed) != nil || len(parsed.Result) > limit {
		return nil, ErrInvalidResponse
	}
	output := make([]productDTO, 0, len(parsed.Result))
	seenSKU := map[int64]struct{}{}
	for _, item := range parsed.Result {
		if item.ProductID < 1 || item.SKUID < 1 || !validText(item.SellerSKUID, 80) || !validText(item.Title, 500) || item.Barcode < 0 {
			return nil, ErrInvalidResponse
		}
		if _, duplicate := seenSKU[item.SKUID]; duplicate {
			return nil, ErrInvalidResponse
		}
		seenSKU[item.SKUID] = struct{}{}
		output = append(output, productDTO{Barcode: item.Barcode, ProductID: item.ProductID, SellerSKUID: item.SellerSKUID, SKUID: item.SKUID, Title: item.Title, Archived: item.IsArchive || item.IsBlocked || !item.IsActive})
	}
	return output, nil
}
