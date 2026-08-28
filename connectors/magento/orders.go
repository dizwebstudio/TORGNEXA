package magento

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
	fingerprint := configuration.fingerprint("orders")
	page, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.OrderPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		query := searchCriteriaQuery(page, request.Limit, nil)
		response, callErr := connector.call(ctx, configuration, credential, "GET", "/orders", query, nil)
		if callErr != nil {
			return callErr
		}
		var result struct {
			Items      []magentoOrder `json:"items"`
			TotalCount int            `json:"total_count"`
		}
		if json.Unmarshal(response.Body, &result) != nil || len(result.Items) > request.Limit {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteOrder, 0, len(result.Items))
		for _, row := range result.Items {
			remoteID := row.EntityID.String()
			if remoteID == "" || !validRemoteText(row.Status, 64) || len(row.Items) > 1000 {
				return ErrInvalidResponse
			}
			created, e := parseMagentoTime(row.CreatedAt)
			if e != nil {
				return ErrInvalidResponse
			}
			updated, e := parseMagentoTime(row.UpdatedAt)
			if e != nil || updated.Before(created) {
				return ErrInvalidResponse
			}
			orderItems := make([]sdk.RemoteOrderItem, 0, len(row.Items))
			for _, line := range row.Items {
				// A parent configurable-product line item carries no
				// quantity of its own (the qty lives on its simple-product
				// child line); skip rows with no meaningful quantity rather
				// than fail the whole order.
				quantity, qErr := line.QtyOrdered.Int64()
				if qErr != nil || quantity < 1 {
					continue
				}
				itemID := line.ItemID.String()
				if itemID == "" || !validRemoteText(line.SKU, 200) {
					return ErrInvalidResponse
				}
				item := sdk.RemoteOrderItem{RemoteID: itemID, VariantRemoteID: line.SKU, Quantity: quantity}
				if item.Validate() != nil {
					return ErrInvalidResponse
				}
				orderItems = append(orderItems, item)
			}
			item := sdk.RemoteOrder{RemoteID: remoteID, ExternalID: row.IncrementID, StatusRemoteID: row.Status, CreatedAt: created.UTC(), UpdatedAt: updated.UTC(), Items: orderItems}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, e := nextCursor(page, request.Limit, result.TotalCount, fingerprint)
		if e != nil {
			return e
		}
		output = sdk.OrderPage{Items: items, NextCursor: next}
		return output.Validate(request.Limit)
	})
	return output, err
}
