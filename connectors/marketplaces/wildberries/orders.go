package wildberries

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type orderCursor struct {
	Next int64 `json:"next"`
}

type assemblyOrdersResponse struct {
	Next   int64 `json:"next"`
	Orders []struct {
		ID        int64    `json:"id"`
		OrderUID  string   `json:"orderUid"`
		RID       string   `json:"rid"`
		CreatedAt string   `json:"createdAt"`
		ChrtID    int64    `json:"chrtId"`
		SKUs      []string `json:"skus"`
	} `json:"orders"`
}

// ReadOrders imports the bounded FBS assembly-order projection. WB's list
// endpoint does not expose a mutable order status, so the connector reports
// the documented assembly state and lets the host reconcile later status
// observations separately.
func (connector *Connector) ReadOrders(ctx context.Context, account sdk.Account, runtime sdk.Runtime, page sdk.PageRequest) (sdk.OrderPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.read") != nil || page.Validate(1000) != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	cursor, err := decodeOrderCursor(page.Cursor)
	if err != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	now := connector.now().UTC()
	query := []QueryParam{
		{Name: "limit", Value: strconv.Itoa(page.Limit)},
		{Name: "next", Value: strconv.FormatInt(cursor.Next, 10)},
		{Name: "dateFrom", Value: strconv.FormatInt(now.Add(-30*24*time.Hour).Unix(), 10)},
		{Name: "dateTo", Value: strconv.FormatInt(now.Unix(), 10)},
	}
	var output sdk.OrderPage
	err = runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "GET", Host: marketplaceHost, Path: "/api/v3/orders", Query: query, Token: secret})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		var parsed assemblyOrdersResponse
		if len(response.Body) == 0 || json.Unmarshal(response.Body, &parsed) != nil || len(parsed.Orders) > page.Limit || parsed.Next < 0 || parsed.Next == cursor.Next && parsed.Next != 0 {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteOrder, 0, len(parsed.Orders))
		seen := make(map[int64]struct{}, len(parsed.Orders))
		for _, remote := range parsed.Orders {
			if remote.ID <= 0 || remote.CreatedAt == "" || remote.ChrtID <= 0 {
				return ErrInvalidResponse
			}
			if _, duplicate := seen[remote.ID]; duplicate {
				return ErrInvalidResponse
			}
			seen[remote.ID] = struct{}{}
			createdAt, parseErr := time.Parse(time.RFC3339Nano, remote.CreatedAt)
			if parseErr != nil {
				return ErrInvalidResponse
			}
			item := sdk.RemoteOrderItem{RemoteID: remote.RID, VariantRemoteID: strconv.FormatInt(remote.ChrtID, 10), Quantity: 1}
			if item.RemoteID == "" {
				item.RemoteID = strconv.FormatInt(remote.ID, 10)
			}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			order := sdk.RemoteOrder{RemoteID: strconv.FormatInt(remote.ID, 10), ExternalID: remote.OrderUID, StatusRemoteID: "assembly", CreatedAt: createdAt.UTC(), UpdatedAt: createdAt.UTC(), Items: []sdk.RemoteOrderItem{item}}
			if order.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, order)
		}
		output = sdk.OrderPage{Items: items}
		if parsed.Next != 0 {
			output.NextCursor, err = encodeOrderCursor(orderCursor{Next: parsed.Next})
			if err != nil {
				return ErrInvalidResponse
			}
		}
		return output.Validate(page.Limit)
	})
	return output, err
}

func encodeOrderCursor(cursor orderCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeOrderCursor(value string) (orderCursor, error) {
	if value == "" {
		return orderCursor{}, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(data) > 128 {
		return orderCursor{}, sdk.ErrInvalidReadRequest
	}
	var cursor orderCursor
	if json.Unmarshal(data, &cursor) != nil || cursor.Next < 1 {
		return orderCursor{}, sdk.ErrInvalidReadRequest
	}
	return cursor, nil
}
