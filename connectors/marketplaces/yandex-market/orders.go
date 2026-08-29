package yandexmarket

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type ordersResponse struct {
	Orders []struct {
		OrderID         int64  `json:"orderId"`
		CampaignID      int64  `json:"campaignId"`
		ProgramType     string `json:"programType"`
		ExternalOrderID string `json:"externalOrderId"`
		Status          string `json:"status"`
		Substatus       string `json:"substatus"`
		CreationDate    string `json:"creationDate"`
		UpdateDate      string `json:"updateDate"`
		Items           []struct {
			ID      int64  `json:"id"`
			OfferID string `json:"offerId"`
			Count   int64  `json:"count"`
		} `json:"items"`
	} `json:"orders"`
	Paging struct {
		NextPageToken string `json:"nextPageToken"`
	} `json:"paging"`
}

func (connector *Connector) ReadOrders(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.OrderPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.read") != nil || request.Validate(50) != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.OrderPage{}, err
	}
	fingerprint := configuration.fingerprint("orders")
	remoteCursor, err := parseCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	query := []QueryParam{{Name: "limit", Value: intString(request.Limit)}}
	if remoteCursor != "" {
		query = append(query, QueryParam{Name: "pageToken", Value: remoteCursor})
	}
	body, _ := json.Marshal(map[string]any{"campaignIds": []int64{configuration.CampaignID}})
	var output sdk.OrderPage
	err = connector.withAPIKey(ctx, runtime, account.SecretReference, func(key []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: businessV1Path(configuration.BusinessID, "/orders"), Query: query, Body: body, APIKey: key})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		page, parseErr := parseOrders(response.Body, request.Limit, configuration, remoteCursor)
		if parseErr != nil {
			return parseErr
		}
		output = page
		return nil
	})
	return output, err
}

func parseOrders(body []byte, limit int, configuration Configuration, previousToken string) (sdk.OrderPage, error) {
	var parsed ordersResponse
	if len(body) == 0 || len(body) > maxBodyBytes || json.Unmarshal(body, &parsed) != nil || len(parsed.Orders) > limit {
		return sdk.OrderPage{}, ErrInvalidResponse
	}
	if parsed.Paging.NextPageToken != "" && (parsed.Paging.NextPageToken == previousToken || !validTokenText(parsed.Paging.NextPageToken)) {
		return sdk.OrderPage{}, ErrInvalidResponse
	}
	items := make([]sdk.RemoteOrder, 0, len(parsed.Orders))
	seen := map[int64]struct{}{}
	for _, remote := range parsed.Orders {
		if remote.OrderID < 1 || remote.CampaignID != configuration.CampaignID || !validText(remote.ProgramType, 64) || !validText(remote.Status, 64) || !validOptionalText(remote.Substatus, 128) || !validOptionalText(remote.ExternalOrderID, 300) || len(remote.Items) > 1000 {
			return sdk.OrderPage{}, ErrInvalidResponse
		}
		if _, duplicate := seen[remote.OrderID]; duplicate {
			return sdk.OrderPage{}, ErrInvalidResponse
		}
		seen[remote.OrderID] = struct{}{}
		createdAt, err := parseUTC(remote.CreationDate)
		if err != nil {
			return sdk.OrderPage{}, ErrInvalidResponse
		}
		updatedAt, err := parseUTC(remote.UpdateDate)
		if err != nil || updatedAt.Before(createdAt) {
			return sdk.OrderPage{}, ErrInvalidResponse
		}
		orderItems := make([]sdk.RemoteOrderItem, 0, len(remote.Items))
		seenItems := map[int64]struct{}{}
		for _, item := range remote.Items {
			if item.ID < 0 || item.Count < 1 || !validText(item.OfferID, 255) {
				return sdk.OrderPage{}, ErrInvalidResponse
			}
			if _, duplicate := seenItems[item.ID]; duplicate {
				return sdk.OrderPage{}, ErrInvalidResponse
			}
			seenItems[item.ID] = struct{}{}
			projection := sdk.RemoteOrderItem{RemoteID: strconv.FormatInt(item.ID, 10), VariantRemoteID: item.OfferID, Quantity: item.Count}
			if projection.Validate() != nil {
				return sdk.OrderPage{}, ErrInvalidResponse
			}
			orderItems = append(orderItems, projection)
		}
		projection := sdk.RemoteOrder{RemoteID: strconv.FormatInt(remote.OrderID, 10), ExternalID: remote.ExternalOrderID, CampaignRemoteID: strconv.FormatInt(remote.CampaignID, 10), ProgramRemoteID: remote.ProgramType, StatusRemoteID: remote.Status, SubstatusRemoteID: remote.Substatus, CreatedAt: createdAt, UpdatedAt: updatedAt, Items: orderItems}
		if projection.Validate() != nil {
			return sdk.OrderPage{}, ErrInvalidResponse
		}
		items = append(items, projection)
	}
	next, err := makeCursor(parsed.Paging.NextPageToken, configuration.fingerprint("orders"))
	if err != nil {
		return sdk.OrderPage{}, ErrInvalidResponse
	}
	page := sdk.OrderPage{Items: items, NextCursor: next}
	if page.Validate(limit) != nil {
		return sdk.OrderPage{}, ErrInvalidResponse
	}
	return page, nil
}
