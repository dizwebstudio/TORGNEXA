package magnitmarket

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type ordersResponse struct {
	NextPageToken string `json:"next_page_token"`
	Orders        []struct {
		CreatedAt           string `json:"created_at"`
		CustomerID          string `json:"customer_id"`
		CustomerOrderNumber string `json:"customer_order_number"`
		CutoffTime          string `json:"cutoff_time"`
		DeliveryRegion      string `json:"delivery_region"`
		OrderID             string `json:"order_id"`
		Status              string `json:"status"`
		WarehouseID         string `json:"warehouse_id"`
		Items               []struct {
			CanceledQuantity int64 `json:"canceled_quantity"`
			Quantity         int64 `json:"quantity"`
			SKUID            int64 `json:"sku_id"`
		} `json:"items"`
	} `json:"orders"`
}

func (connector *Connector) ReadOrders(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.OrderPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.read") != nil || request.Validate(100) != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.OrderPage{}, err
	}
	fingerprint := configuration.fingerprint("orders")
	token, from, to, err := parseOrderCursor(request.Cursor, fingerprint, connector.now().UTC(), configuration.OrderWindowDays)
	if err != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	body, _ := json.Marshal(map[string]any{
		"page_size":  request.Limit,
		"page_token": token,
		"created_at": map[string]any{"from": from.Format("2006-01-02T15:04:05Z07:00"), "to": to.Format("2006-01-02T15:04:05Z07:00")},
		"dir":        "ASC",
	})
	var output sdk.OrderPage
	err = connector.withAPIKey(ctx, runtime, account.SecretReference, func(key []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/api/seller/v1/orders/list", Body: body, APIKey: key})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		page, parseErr := parseOrders(response.Body, request.Limit, token, from, to, fingerprint)
		if parseErr != nil {
			return parseErr
		}
		output = page
		return nil
	})
	return output, err
}

func parseOrders(body []byte, limit int, previousToken string, from, to time.Time, fingerprint string) (sdk.OrderPage, error) {
	var parsed ordersResponse
	if len(body) == 0 || len(body) > maxBodyBytes || decodeUseNumber(body, &parsed) != nil || len(parsed.Orders) > limit {
		return sdk.OrderPage{}, ErrInvalidResponse
	}
	if parsed.NextPageToken != "" && (parsed.NextPageToken == previousToken || !validTokenText(parsed.NextPageToken)) {
		return sdk.OrderPage{}, ErrInvalidResponse
	}
	output := make([]sdk.RemoteOrder, 0, len(parsed.Orders))
	seenOrders := map[string]struct{}{}
	for _, order := range parsed.Orders {
		if !validText(order.OrderID, 64) || !validText(order.CustomerOrderNumber, 300) || !validText(order.Status, 64) || !validText(order.WarehouseID, 128) || len(order.Items) > 1000 {
			return sdk.OrderPage{}, ErrInvalidResponse
		}
		if _, duplicate := seenOrders[order.OrderID]; duplicate {
			return sdk.OrderPage{}, ErrInvalidResponse
		}
		seenOrders[order.OrderID] = struct{}{}
		createdAt, err := parseUTC(order.CreatedAt)
		if err != nil || createdAt.Before(from) || !createdAt.Before(to) {
			return sdk.OrderPage{}, ErrInvalidResponse
		}
		items := make([]sdk.RemoteOrderItem, 0, len(order.Items))
		for index, item := range order.Items {
			if item.SKUID < 1 || item.Quantity < 0 || item.CanceledQuantity < 0 || item.CanceledQuantity > item.Quantity {
				return sdk.OrderPage{}, ErrInvalidResponse
			}
			if item.Quantity == 0 {
				continue
			}
			remoteItem := sdk.RemoteOrderItem{RemoteID: order.OrderID + ":" + strconv.Itoa(index), VariantRemoteID: strconv.FormatInt(item.SKUID, 10), Quantity: item.Quantity}
			if remoteItem.Validate() != nil {
				return sdk.OrderPage{}, ErrInvalidResponse
			}
			items = append(items, remoteItem)
		}
		remoteOrder := sdk.RemoteOrder{
			RemoteID:        order.OrderID,
			ExternalID:      order.CustomerOrderNumber,
			ProgramRemoteID: "FBS",
			StatusRemoteID:  order.Status,
			CreatedAt:       createdAt,
			UpdatedAt:       createdAt,
			Items:           items,
		}
		if remoteOrder.Validate() != nil {
			return sdk.OrderPage{}, ErrInvalidResponse
		}
		output = append(output, remoteOrder)
	}
	next, err := makeOrderCursor(parsed.NextPageToken, from, to, fingerprint)
	if err != nil {
		return sdk.OrderPage{}, ErrInvalidResponse
	}
	page := sdk.OrderPage{Items: output, NextCursor: next}
	if page.Validate(limit) != nil {
		return sdk.OrderPage{}, ErrInvalidResponse
	}
	return page, nil
}
