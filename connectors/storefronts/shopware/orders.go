package shopware

import (
	"context"
	"encoding/json"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func (connector *Connector) ReadOrders(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.OrderPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.read") != nil || request.Validate(100) != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.OrderPage{}, err
	}
	fingerprint := configuration.fingerprint("orders")
	page, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.OrderPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		body, marshalErr := json.Marshal(map[string]any{
			"page": page, "limit": request.Limit,
			"sort":         []map[string]any{{"field": "updatedAt", "order": "ASC"}},
			"associations": map[string]any{"stateMachineState": map[string]any{}, "lineItems": map[string]any{}},
		})
		if marshalErr != nil {
			return marshalErr
		}
		response, callErr := connector.call(ctx, configuration, account.ID, credential, "POST", "/search/order", nil, body)
		if callErr != nil {
			return callErr
		}
		var result shopwareSearchPage[shopwareOrder]
		if json.Unmarshal(response.Body, &result) != nil || len(result.Data) > request.Limit {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteOrder, 0, len(result.Data))
		for _, row := range result.Data {
			if row.ID == "" || row.StateMachineState == nil || !validRemoteText(row.StateMachineState.TechnicalName, 64) || len(row.LineItems) > 1000 {
				return ErrInvalidResponse
			}
			created, e := time.Parse(time.RFC3339, row.CreatedAt)
			if e != nil {
				return ErrInvalidResponse
			}
			updated, e := time.Parse(time.RFC3339, row.UpdatedAt)
			if e != nil || updated.Before(created) {
				return ErrInvalidResponse
			}
			orderItems := make([]sdk.RemoteOrderItem, 0, len(row.LineItems))
			for _, line := range row.LineItems {
				quantity, qErr := line.Quantity.Int64()
				// A line item with no referencedId (a custom/manual line, e.g.
				// a discount or shipping surcharge) has no catalog product to
				// reconcile against; skip it rather than fail the whole order.
				if line.ReferencedID == "" {
					continue
				}
				if line.ID == "" || qErr != nil || quantity < 1 {
					return ErrInvalidResponse
				}
				item := sdk.RemoteOrderItem{RemoteID: line.ID, VariantRemoteID: line.ReferencedID, Quantity: quantity}
				if item.Validate() != nil {
					return ErrInvalidResponse
				}
				orderItems = append(orderItems, item)
			}
			item := sdk.RemoteOrder{RemoteID: row.ID, ExternalID: row.OrderNumber, StatusRemoteID: row.StateMachineState.TechnicalName, CreatedAt: created.UTC(), UpdatedAt: updated.UTC(), Items: orderItems}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, e := nextCursor(page, request.Limit, result.Total, fingerprint)
		if e != nil {
			return e
		}
		output = sdk.OrderPage{Items: items, NextCursor: next}
		return output.Validate(request.Limit)
	})
	return output, err
}
