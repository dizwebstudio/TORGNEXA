package aliexpressru

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type productListResponse struct {
	Data []struct {
		ID           string `json:"id"`
		AliUpdatedAt string `json:"ali_updated_at"`
		Subject      string `json:"subject"`
		SKU          []struct {
			ID    string `json:"id"`
			SKUID string `json:"sku_id"`
			Code  string `json:"code"`
		} `json:"sku"`
	} `json:"data"`
	Error json.RawMessage `json:"error"`
}

func (connector *Connector) ReadProducts(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ProductPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || request.Validate(100) != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	lastProductID, err := parseProductCursor(request.Cursor)
	if err != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	body, _ := json.Marshal(map[string]any{"filter": map[string]any{}, "last_product_id": lastProductID, "limit": strconv.Itoa(request.Limit)})
	var output sdk.ProductPage
	err = connector.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: productsPath, Body: body, XAuthToken: token})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		products, parseErr := parseProducts(response.Body, request.Limit)
		if parseErr != nil {
			return parseErr
		}
		next := ""
		if len(products) == request.Limit && len(products) > 0 {
			next, parseErr = makeProductCursor(products[len(products)-1].RemoteID)
			if parseErr != nil {
				return parseErr
			}
		}
		output = sdk.ProductPage{Items: products, NextCursor: next}
		if output.Validate(request.Limit) != nil {
			return ErrInvalidResponse
		}
		return nil
	})
	return output, err
}

func parseProducts(body []byte, limit int) ([]sdk.RemoteProduct, error) {
	if len(body) == 0 || len(body) > maxBodyBytes || limit < 1 || limit > 100 {
		return nil, ErrInvalidResponse
	}
	var parsed productListResponse
	if decodeUseNumber(body, &parsed) != nil || len(parsed.Data) > limit || hasRemoteError(parsed.Error) {
		return nil, ErrInvalidResponse
	}
	output := make([]sdk.RemoteProduct, 0, len(parsed.Data))
	seenProducts := map[string]struct{}{}
	seenVariants := map[string]struct{}{}
	for _, item := range parsed.Data {
		if !validRemoteText(item.ID, 128) || !validRemoteText(item.Subject, 500) || len(item.SKU) < 1 || len(item.SKU) > 1000 {
			return nil, ErrInvalidResponse
		}
		if _, duplicate := seenProducts[item.ID]; duplicate {
			return nil, ErrInvalidResponse
		}
		seenProducts[item.ID] = struct{}{}
		updatedAt, err := parseUTC(item.AliUpdatedAt)
		if err != nil {
			return nil, ErrInvalidResponse
		}
		variants := make([]sdk.RemoteVariant, 0, len(item.SKU))
		sellerSKU := ""
		for _, sku := range item.SKU {
			remoteVariantID := sku.SKUID
			if remoteVariantID == "" {
				remoteVariantID = sku.ID
			}
			if !validRemoteText(remoteVariantID, 128) || !validRemoteText(sku.Code, 200) {
				return nil, ErrInvalidResponse
			}
			if _, duplicate := seenVariants[remoteVariantID]; duplicate {
				return nil, ErrInvalidResponse
			}
			seenVariants[remoteVariantID] = struct{}{}
			if sellerSKU == "" {
				sellerSKU = sku.Code
			}
			variant := sdk.RemoteVariant{RemoteID: remoteVariantID, SKUs: []string{sku.Code}}
			variants = append(variants, variant)
		}
		product := sdk.RemoteProduct{RemoteID: item.ID, SellerSKU: sellerSKU, Title: item.Subject, UpdatedAt: updatedAt, Variants: variants}
		if product.Validate() != nil {
			return nil, ErrInvalidResponse
		}
		output = append(output, product)
	}
	return output, nil
}

func hasRemoteError(raw json.RawMessage) bool {
	trimmed := string(raw)
	return trimmed != "" && trimmed != "null" && trimmed != "{}"
}
