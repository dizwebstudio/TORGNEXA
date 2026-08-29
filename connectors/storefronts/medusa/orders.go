package medusa

import (
	"context"
	"encoding/json"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const orderListFields = "id,display_id,status,currency_code,created_at,updated_at,items.id,items.variant_id,items.product_id,items.quantity"

func (connector *Connector) ReadOrders(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.OrderPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.read") != nil || request.Validate(100) != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.OrderPage{}, err
	}
	fingerprint := configuration.fingerprint("orders")
	offset, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.OrderPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		response, callErr := connector.call(ctx, configuration, credential, "GET", "/orders", []QueryParam{{Name: "offset", Value: intString(offset)}, {Name: "limit", Value: intString(request.Limit)}, {Name: "order", Value: "updated_at"}, {Name: "fields", Value: orderListFields}}, nil)
		if callErr != nil {
			return callErr
		}
		var page struct {
			Orders []medusaOrder `json:"orders"`
			Count  int           `json:"count"`
		}
		if json.Unmarshal(response.Body, &page) != nil || len(page.Orders) > request.Limit {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteOrder, 0, len(page.Orders))
		for _, row := range page.Orders {
			if row.ID == "" || !validRemoteText(row.Status, 64) || len(row.Items) > 1000 {
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
			orderItems := make([]sdk.RemoteOrderItem, 0, len(row.Items))
			for _, line := range row.Items {
				quantity, qErr := line.Quantity.Int64()
				if line.ID == "" || line.ProductID == "" || line.VariantID == "" || qErr != nil || quantity < 1 {
					return ErrInvalidResponse
				}
				item := sdk.RemoteOrderItem{RemoteID: line.ID, VariantRemoteID: variantRemoteID(line.ProductID, line.VariantID), Quantity: quantity}
				if item.Validate() != nil {
					return ErrInvalidResponse
				}
				orderItems = append(orderItems, item)
			}
			item := sdk.RemoteOrder{RemoteID: row.ID, ExternalID: row.DisplayID.String(), StatusRemoteID: row.Status, CreatedAt: created.UTC(), UpdatedAt: updated.UTC(), Items: orderItems}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, e := nextCursor(offset, request.Limit, page.Count, fingerprint)
		if e != nil {
			return e
		}
		output = sdk.OrderPage{Items: items, NextCursor: next}
		return output.Validate(request.Limit)
	})
	return output, err
}
