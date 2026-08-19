package yandexmarket

import (
	"context"
	"encoding/json"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type offerMappingsResponse struct {
	Status string `json:"status"`
	Result *struct {
		Paging struct {
			NextPageToken string `json:"nextPageToken"`
		} `json:"paging"`
		OfferMappings []struct {
			Offer struct {
				OfferID    string   `json:"offerId"`
				Name       string   `json:"name"`
				Vendor     string   `json:"vendor"`
				Barcodes   []string `json:"barcodes"`
				Archived   bool     `json:"archived"`
				BasicPrice struct {
					UpdatedAt string `json:"updatedAt"`
				} `json:"basicPrice"`
			} `json:"offer"`
			Mapping struct {
				MarketSKU int64 `json:"marketSku"`
			} `json:"mapping"`
		} `json:"offerMappings"`
	} `json:"result"`
}

func (connector *Connector) ReadProducts(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ProductPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || request.Validate(100) != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ProductPage{}, err
	}
	fingerprint := configuration.fingerprint("products")
	remoteCursor, err := parseCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	query := []QueryParam{{Name: "limit", Value: intString(request.Limit)}}
	if remoteCursor != "" {
		query = append(query, QueryParam{Name: "pageToken", Value: remoteCursor})
	}
	var output sdk.ProductPage
	err = connector.withAPIKey(ctx, runtime, account.SecretReference, func(key []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: businessPath(configuration.BusinessID, "/offer-mappings"), Query: query, Body: []byte(`{}`), APIKey: key})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		page, parseErr := parseProducts(response.Body, request.Limit, configuration, remoteCursor)
		if parseErr != nil {
			return parseErr
		}
		output = page
		return nil
	})
	return output, err
}

func parseProducts(body []byte, limit int, configuration Configuration, previousToken string) (sdk.ProductPage, error) {
	var parsed offerMappingsResponse
	if len(body) == 0 || len(body) > maxBodyBytes || json.Unmarshal(body, &parsed) != nil || parsed.Status != "OK" || parsed.Result == nil || len(parsed.Result.OfferMappings) > limit {
		return sdk.ProductPage{}, ErrInvalidResponse
	}
	if parsed.Result.Paging.NextPageToken != "" && (parsed.Result.Paging.NextPageToken == previousToken || !validTokenText(parsed.Result.Paging.NextPageToken)) {
		return sdk.ProductPage{}, ErrInvalidResponse
	}
	items := make([]sdk.RemoteProduct, 0, len(parsed.Result.OfferMappings))
	seen := make(map[string]struct{}, len(parsed.Result.OfferMappings))
	for _, mapping := range parsed.Result.OfferMappings {
		offer := mapping.Offer
		if !validText(offer.OfferID, 255) || !validText(offer.Name, 500) || !validOptionalText(offer.Vendor, 300) || len(offer.Barcodes) > 100 {
			return sdk.ProductPage{}, ErrInvalidResponse
		}
		if _, duplicate := seen[offer.OfferID]; duplicate {
			return sdk.ProductPage{}, ErrInvalidResponse
		}
		seen[offer.OfferID] = struct{}{}
		updatedAt, err := parseUTC(offer.BasicPrice.UpdatedAt)
		if err != nil {
			return sdk.ProductPage{}, ErrInvalidResponse
		}
		barcodes := make([]string, 0, len(offer.Barcodes))
		seenBarcode := map[string]struct{}{}
		for _, barcode := range offer.Barcodes {
			if !validText(barcode, 200) {
				return sdk.ProductPage{}, ErrInvalidResponse
			}
			if _, duplicate := seenBarcode[barcode]; duplicate {
				return sdk.ProductPage{}, ErrInvalidResponse
			}
			seenBarcode[barcode] = struct{}{}
			barcodes = append(barcodes, barcode)
		}
		item := sdk.RemoteProduct{RemoteID: offer.OfferID, SellerSKU: offer.OfferID, Title: offer.Name, Brand: offer.Vendor, UpdatedAt: updatedAt, Variants: []sdk.RemoteVariant{{RemoteID: offer.OfferID, SKUs: barcodes}}}
		if item.Validate() != nil {
			return sdk.ProductPage{}, ErrInvalidResponse
		}
		items = append(items, item)
	}
	next, err := makeCursor(parsed.Result.Paging.NextPageToken, configuration.fingerprint("products"))
	if err != nil {
		return sdk.ProductPage{}, ErrInvalidResponse
	}
	page := sdk.ProductPage{Items: items, NextCursor: next}
	if page.Validate(limit) != nil {
		return sdk.ProductPage{}, ErrInvalidResponse
	}
	return page, nil
}
