package woocommerce

import (
	"context"
	"encoding/json"

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
	page, err := decodePageCursor(request.Cursor, configuration.fingerprint("orders"))
	if err != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.OrderPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		response, callErr := connector.call(ctx, configuration, credential, "GET", "/orders", []QueryParam{{Name: "page", Value: intString(int64(page))}, {Name: "per_page", Value: intString(int64(request.Limit))}, {Name: "orderby", Value: "modified"}, {Name: "order", Value: "asc"}}, nil)
		if callErr != nil {
			return callErr
		}
		var rows []wooOrder
		if json.Unmarshal(response.Body, &rows) != nil || len(rows) > request.Limit {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteOrder, 0, len(rows))
		for _, row := range rows {
			if row.ID < 1 || !validRemoteText(row.Status, 64) || len(row.LineItems) > 1000 {
				return ErrInvalidResponse
			}
			created, e := parseWooTime(row.DateCreatedGMT)
			if e != nil {
				return e
			}
			updated, e := parseWooTime(row.DateModifiedGMT)
			if e != nil || updated.Before(created) {
				return ErrInvalidResponse
			}
			orderItems := make([]sdk.RemoteOrderItem, 0, len(row.LineItems))
			for _, line := range row.LineItems {
				if line.ID < 1 || line.ProductID < 1 || line.Quantity < 1 {
					return ErrInvalidResponse
				}
				remoteVariant := variantRemoteID(line.ProductID, line.VariationID)
				item := sdk.RemoteOrderItem{RemoteID: intString(line.ID), VariantRemoteID: remoteVariant, Quantity: line.Quantity}
				if item.Validate() != nil {
					return ErrInvalidResponse
				}
				orderItems = append(orderItems, item)
			}
			item := sdk.RemoteOrder{RemoteID: intString(row.ID), ExternalID: row.Number, StatusRemoteID: row.Status, CreatedAt: created, UpdatedAt: updated, Items: orderItems}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, e := nextCursor(page, request.Limit, len(rows), response.TotalPages, configuration.fingerprint("orders"))
		if e != nil {
			return e
		}
		output = sdk.OrderPage{Items: items, NextCursor: next}
		return output.Validate(request.Limit)
	})
	return output, err
}
