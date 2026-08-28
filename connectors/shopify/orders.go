package shopify

import (
	"context"
	"encoding/json"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// shopifyOrderStatus derives a single lifecycle status from Shopify's
// timestamp-based order state, since Shopify has no single writable "status"
// field the way WooCommerce does — financial_status and fulfillment_status
// are independent axes, and fulfillment_status is read-only on the Order
// resource itself (see write.go's WriteOrderStatus for the exact writable
// subset this status model represents: cancelled/closed/open only).
func shopifyOrderStatus(order shopifyOrder) string {
	if order.CancelledAt != nil && *order.CancelledAt != "" {
		return "cancelled"
	}
	if order.ClosedAt != nil && *order.ClosedAt != "" {
		return "closed"
	}
	return "open"
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
	pageInfo, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.OrderPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		// status=any: Shopify's default orders.json filter is open orders
		// only, and this filter can only travel on the first, unpaginated
		// request (see cursor.go's listQuery) — Shopify carries it forward
		// inside the opaque page_info token for later pages.
		response, callErr := connector.call(ctx, configuration, credential, "GET", "/orders.json", listQuery(pageInfo, request.Limit, QueryParam{Name: "status", Value: "any"}, QueryParam{Name: "order", Value: "updated_at asc"}), nil)
		if callErr != nil {
			return callErr
		}
		var page struct {
			Orders []shopifyOrder `json:"orders"`
		}
		if json.Unmarshal(response.Body, &page) != nil || len(page.Orders) > request.Limit {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteOrder, 0, len(page.Orders))
		for _, row := range page.Orders {
			if row.ID < 1 || len(row.LineItems) > 1000 {
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
				if line.ID < 1 || line.Quantity < 1 {
					return ErrInvalidResponse
				}
				// A custom line item (gift wrap, setup fee, ...) has no
				// variant and cannot be reconciled against the catalog;
				// skip it rather than fail the whole order.
				if line.VariantID < 1 {
					continue
				}
				item := sdk.RemoteOrderItem{RemoteID: intString(line.ID), VariantRemoteID: variantRemoteID(line.VariantID), Quantity: line.Quantity}
				if item.Validate() != nil {
					return ErrInvalidResponse
				}
				orderItems = append(orderItems, item)
			}
			item := sdk.RemoteOrder{RemoteID: intString(row.ID), ExternalID: row.Name, StatusRemoteID: shopifyOrderStatus(row), CreatedAt: created.UTC(), UpdatedAt: updated.UTC(), Items: orderItems}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, e := nextCursor(response.NextPageInfo, fingerprint)
		if e != nil {
			return e
		}
		output = sdk.OrderPage{Items: items, NextCursor: next}
		return output.Validate(request.Limit)
	})
	return output, err
}
