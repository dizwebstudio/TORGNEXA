package ozon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type productCursor struct {
	LastID string `json:"last_id"`
}

type productListRequest struct {
	Filter struct {
		Visibility string `json:"visibility"`
	} `json:"filter"`
	LastID string `json:"last_id"`
	Limit  int    `json:"limit"`
}

type productListResponse struct {
	Result struct {
		Items []struct {
			ProductID int64  `json:"product_id"`
			OfferID   string `json:"offer_id"`
		} `json:"items"`
		Total  int64  `json:"total"`
		LastID string `json:"last_id"`
	} `json:"result"`
}

type productInfoRequest struct {
	ProductIDs []int64 `json:"product_id"`
}

type productInfoResponse struct {
	Items []struct {
		ID        int64    `json:"id"`
		Name      string   `json:"name"`
		OfferID   string   `json:"offer_id"`
		Barcodes  []string `json:"barcodes"`
		UpdatedAt string   `json:"updated_at"`
	} `json:"items"`
}

func (connector *Connector) ReadProducts(ctx context.Context, account sdk.Account, runtime sdk.Runtime, page sdk.PageRequest) (sdk.ProductPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || page.Validate(100) != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	cursor, err := decodeCursor(page.Cursor)
	if err != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	var request productListRequest
	request.Filter.Visibility = "ALL"
	request.LastID = cursor.LastID
	request.Limit = page.Limit
	listBody, _ := json.Marshal(request)

	var output sdk.ProductPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(clientID, apiKey []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/v3/product/list", Body: listBody, ClientID: clientID, APIKey: apiKey})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		listed, parseErr := parseProductList(response.Body, page.Limit, cursor.LastID)
		if parseErr != nil {
			return parseErr
		}
		if len(listed.Result.Items) == 0 {
			return nil
		}

		productIDs := make([]int64, 0, len(listed.Result.Items))
		expectedOffers := make(map[int64]string, len(listed.Result.Items))
		for _, item := range listed.Result.Items {
			productIDs = append(productIDs, item.ProductID)
			expectedOffers[item.ProductID] = item.OfferID
		}
		infoBody, _ := json.Marshal(productInfoRequest{ProductIDs: productIDs})
		infoResponse, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/v3/product/info/list", Body: infoBody, ClientID: clientID, APIKey: apiKey})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(infoResponse); remote != nil {
			return remote
		}
		parsedInfo, parseErr := parseProductInfo(infoResponse.Body, expectedOffers)
		if parseErr != nil {
			return parseErr
		}
		byID := make(map[int64]sdk.RemoteProduct, len(parsedInfo.Items))
		for _, item := range parsedInfo.Items {
			updated, parseErr := time.Parse(time.RFC3339Nano, item.UpdatedAt)
			if parseErr != nil {
				return ErrInvalidResponse
			}
			product := sdk.RemoteProduct{
				RemoteID: strconv.FormatInt(item.ID, 10), SellerSKU: item.OfferID, Title: item.Name, UpdatedAt: updated.UTC(),
				Variants: []sdk.RemoteVariant{{RemoteID: item.OfferID, SKUs: append([]string(nil), item.Barcodes...)}},
			}
			if product.Validate() != nil {
				return ErrInvalidResponse
			}
			byID[item.ID] = product
		}
		output.Items = make([]sdk.RemoteProduct, 0, len(listed.Result.Items))
		for _, listedItem := range listed.Result.Items {
			product, ok := byID[listedItem.ProductID]
			if !ok {
				return ErrInvalidResponse
			}
			output.Items = append(output.Items, product)
		}
		if listed.Result.LastID != "" {
			output.NextCursor, parseErr = encodeCursor(productCursor{LastID: listed.Result.LastID})
			if parseErr != nil {
				return ErrInvalidResponse
			}
		}
		return output.Validate(page.Limit)
	})
	return output, err
}

func parseProductList(body []byte, limit int, previousLastID string) (productListResponse, error) {
	var parsed productListResponse
	if len(body) == 0 || len(body) > maxBodyBytes || json.Unmarshal(body, &parsed) != nil || len(parsed.Result.Items) > limit || parsed.Result.Total < int64(len(parsed.Result.Items)) {
		return productListResponse{}, ErrInvalidResponse
	}
	if parsed.Result.LastID != "" {
		if parsed.Result.LastID == previousLastID || !validOpaqueRemoteCursor(parsed.Result.LastID) {
			return productListResponse{}, ErrInvalidResponse
		}
	}
	seenIDs := make(map[int64]struct{}, len(parsed.Result.Items))
	seenOffers := make(map[string]struct{}, len(parsed.Result.Items))
	for _, item := range parsed.Result.Items {
		if item.ProductID <= 0 || !validRemoteText(item.OfferID, 200) {
			return productListResponse{}, ErrInvalidResponse
		}
		if _, duplicate := seenIDs[item.ProductID]; duplicate {
			return productListResponse{}, ErrInvalidResponse
		}
		if _, duplicate := seenOffers[item.OfferID]; duplicate {
			return productListResponse{}, ErrInvalidResponse
		}
		seenIDs[item.ProductID] = struct{}{}
		seenOffers[item.OfferID] = struct{}{}
	}
	return parsed, nil
}

func parseProductInfo(body []byte, expected map[int64]string) (productInfoResponse, error) {
	var parsed productInfoResponse
	if len(body) == 0 || len(body) > maxBodyBytes || json.Unmarshal(body, &parsed) != nil || len(parsed.Items) != len(expected) {
		return productInfoResponse{}, ErrInvalidResponse
	}
	seen := make(map[int64]struct{}, len(parsed.Items))
	for _, item := range parsed.Items {
		offer, ok := expected[item.ID]
		if !ok || item.ID <= 0 || item.OfferID != offer || !validRemoteText(item.Name, 500) || !validRemoteText(item.OfferID, 200) || len(item.Barcodes) > 100 {
			return productInfoResponse{}, ErrInvalidResponse
		}
		if _, duplicate := seen[item.ID]; duplicate {
			return productInfoResponse{}, ErrInvalidResponse
		}
		seen[item.ID] = struct{}{}
		if _, err := time.Parse(time.RFC3339Nano, item.UpdatedAt); err != nil {
			return productInfoResponse{}, ErrInvalidResponse
		}
		barcodeSeen := map[string]struct{}{}
		for _, barcode := range item.Barcodes {
			if !validRemoteText(barcode, 200) {
				return productInfoResponse{}, ErrInvalidResponse
			}
			if _, duplicate := barcodeSeen[barcode]; duplicate {
				return productInfoResponse{}, ErrInvalidResponse
			}
			barcodeSeen[barcode] = struct{}{}
		}
	}
	return parsed, nil
}

func encodeCursor(cursor productCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeCursor(value string) (productCursor, error) {
	if value == "" {
		return productCursor{}, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(data) > 4096 {
		return productCursor{}, sdk.ErrInvalidReadRequest
	}
	var cursor productCursor
	if json.Unmarshal(data, &cursor) != nil || !validOpaqueRemoteCursor(cursor.LastID) {
		return productCursor{}, sdk.ErrInvalidReadRequest
	}
	return cursor, nil
}

func validOpaqueRemoteCursor(value string) bool {
	if value == "" || len(value) > 2048 || !utf8.ValidString(value) || value != strings.TrimSpace(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validRemoteText(value string, max int) bool {
	if value == "" || value != strings.TrimSpace(value) || !utf8.ValidString(value) || utf8.RuneCountInString(value) > max {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
