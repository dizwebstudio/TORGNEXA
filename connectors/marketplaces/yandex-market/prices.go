package yandexmarket

import (
	"context"
	"encoding/json"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type priceDTO struct {
	Value        json.Number `json:"value"`
	DiscountBase json.Number `json:"discountBase"`
	CurrencyID   string      `json:"currencyId"`
	VAT          int64       `json:"vat"`
	UpdatedAt    string      `json:"updatedAt"`
}

type campaignPricesResponse struct {
	Status string `json:"status"`
	Result *struct {
		Offers []struct {
			OfferID   string   `json:"offerId"`
			Price     priceDTO `json:"price"`
			UpdatedAt string   `json:"updatedAt"`
		} `json:"offers"`
		Paging struct {
			NextPageToken string `json:"nextPageToken"`
		} `json:"paging"`
	} `json:"result"`
}

type businessPricesResponse struct {
	Status string `json:"status"`
	Result *struct {
		OfferMappings []struct {
			Offer struct {
				OfferID    string   `json:"offerId"`
				BasicPrice priceDTO `json:"basicPrice"`
			} `json:"offer"`
		} `json:"offerMappings"`
		Paging struct {
			NextPageToken string `json:"nextPageToken"`
		} `json:"paging"`
	} `json:"result"`
}

func (connector *Connector) ReadPrices(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.PricePage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.read") != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.PricePage{}, err
	}
	maxLimit := 500
	if configuration.PriceMode == PriceBusinessWide {
		maxLimit = 100
	}
	if request.Validate(maxLimit) != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	fingerprint := configuration.fingerprint("prices")
	remoteCursor, err := parseCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	query := []QueryParam{{Name: "limit", Value: intString(request.Limit)}}
	if remoteCursor != "" {
		query = append(query, QueryParam{Name: "pageToken", Value: remoteCursor})
	}
	path := campaignPath(configuration.CampaignID, "/offer-prices")
	if configuration.PriceMode == PriceBusinessWide {
		path = businessPath(configuration.BusinessID, "/offer-mappings")
	}
	var output sdk.PricePage
	err = connector.withAPIKey(ctx, runtime, account.SecretReference, func(key []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: path, Query: query, Body: []byte(`{}`), APIKey: key})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		page, parseErr := parsePrices(response.Body, request.Limit, configuration, remoteCursor)
		if parseErr != nil {
			return parseErr
		}
		output = page
		return nil
	})
	return output, err
}

func parsePrices(body []byte, limit int, configuration Configuration, previousToken string) (sdk.PricePage, error) {
	if len(body) == 0 || len(body) > maxBodyBytes {
		return sdk.PricePage{}, ErrInvalidResponse
	}
	var items []sdk.RemotePrice
	var nextToken string
	if configuration.PriceMode == PriceCampaignUnique {
		var parsed campaignPricesResponse
		if decodeUseNumber(body, &parsed) != nil || parsed.Status != "OK" || parsed.Result == nil || len(parsed.Result.Offers) > limit {
			return sdk.PricePage{}, ErrInvalidResponse
		}
		items = make([]sdk.RemotePrice, 0, len(parsed.Result.Offers))
		for _, offer := range parsed.Result.Offers {
			price := offer.Price
			if price.UpdatedAt == "" {
				price.UpdatedAt = offer.UpdatedAt
			}
			item, err := toRemotePrice(offer.OfferID, price)
			if err != nil {
				return sdk.PricePage{}, err
			}
			items = append(items, item)
		}
		nextToken = parsed.Result.Paging.NextPageToken
	} else {
		var parsed businessPricesResponse
		if decodeUseNumber(body, &parsed) != nil || parsed.Status != "OK" || parsed.Result == nil || len(parsed.Result.OfferMappings) > limit {
			return sdk.PricePage{}, ErrInvalidResponse
		}
		items = make([]sdk.RemotePrice, 0, len(parsed.Result.OfferMappings))
		for _, mapping := range parsed.Result.OfferMappings {
			item, err := toRemotePrice(mapping.Offer.OfferID, mapping.Offer.BasicPrice)
			if err != nil {
				return sdk.PricePage{}, err
			}
			items = append(items, item)
		}
		nextToken = parsed.Result.Paging.NextPageToken
	}
	if nextToken != "" && (nextToken == previousToken || !validTokenText(nextToken)) {
		return sdk.PricePage{}, ErrInvalidResponse
	}
	next, err := makeCursor(nextToken, configuration.fingerprint("prices"))
	if err != nil {
		return sdk.PricePage{}, ErrInvalidResponse
	}
	page := sdk.PricePage{Items: items, NextCursor: next}
	if page.Validate(limit) != nil {
		return sdk.PricePage{}, ErrInvalidResponse
	}
	return page, nil
}

func toRemotePrice(offerID string, price priceDTO) (sdk.RemotePrice, error) {
	if !validText(offerID, 255) || !positiveNumber(price.Value.String()) || !validCurrencyCode(price.CurrencyID) || price.VAT < 0 || price.VAT > 1000 {
		return sdk.RemotePrice{}, ErrInvalidResponse
	}
	compare := price.DiscountBase.String()
	if compare == "0" || compare == "" {
		compare = ""
	} else if !positiveNumber(compare) {
		return sdk.RemotePrice{}, ErrInvalidResponse
	}
	updatedAt, err := parseUTC(price.UpdatedAt)
	if err != nil {
		return sdk.RemotePrice{}, ErrInvalidResponse
	}
	vat := ""
	if price.VAT != 0 {
		vat = int64String(price.VAT)
	}
	item := sdk.RemotePrice{VariantRemoteID: offerID, Value: price.Value.String(), CompareAt: compare, Currency: price.CurrencyID, VATRemoteID: vat, UpdatedAt: updatedAt}
	if item.Validate() != nil {
		return sdk.RemotePrice{}, ErrInvalidResponse
	}
	return item, nil
}

func decodeUseNumber(body []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) == nil {
		return ErrInvalidResponse
	}
	return nil
}

func positiveNumber(value string) bool {
	if value == "" || strings.ContainsAny(value, "eE+-") {
		return false
	}
	seenDigit := false
	nonZero := false
	dots := 0
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			seenDigit = true
			if r != '0' {
				nonZero = true
			}
		case r == '.':
			dots++
			if dots > 1 {
				return false
			}
		default:
			return false
		}
	}
	return seenDigit && nonZero && len(value) <= 28 && value[0] != '.' && value[len(value)-1] != '.'
}

func validCurrencyCode(value string) bool {
	if len(value) < 3 || len(value) > 8 || value != strings.ToUpper(value) {
		return false
	}
	for _, r := range value {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
