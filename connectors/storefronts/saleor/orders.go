package saleor

import (
	"context"
	"encoding/json"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const listOrdersQuery = `query ListOrders($channel: String!, $first: Int!, $after: String) {
  orders(channel: $channel, first: $first, after: $after) {
    edges {
      cursor
      node {
        id number status created updatedAt
        lines { id productVariantId quantity }
      }
    }
    pageInfo { hasNextPage endCursor }
  }
}`

type orderLineNode struct {
	ID               string  `json:"id"`
	ProductVariantID *string `json:"productVariantId"`
	Quantity         int     `json:"quantity"`
}

type orderNode struct {
	ID        string          `json:"id"`
	Number    string          `json:"number"`
	Status    string          `json:"status"`
	Created   string          `json:"created"`
	UpdatedAt string          `json:"updatedAt"`
	Lines     []orderLineNode `json:"lines"`
}

type orderConnection struct {
	Edges []struct {
		Cursor string    `json:"cursor"`
		Node   orderNode `json:"node"`
	} `json:"edges"`
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
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
	after, err := decodeRelayCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.OrderPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		variables := map[string]any{"channel": configuration.Channel, "first": request.Limit}
		if after != "" {
			variables["after"] = after
		}
		data, callErr := connector.graphql(ctx, configuration, credential, listOrdersQuery, variables)
		if callErr != nil {
			return callErr
		}
		var result struct {
			Orders orderConnection `json:"orders"`
		}
		if json.Unmarshal(data, &result) != nil || len(result.Orders.Edges) > request.Limit {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteOrder, 0, len(result.Orders.Edges))
		for _, edge := range result.Orders.Edges {
			node := edge.Node
			if !validRemoteText(node.ID, 512) || !validRemoteText(node.Status, 64) || len(node.Lines) > 1000 {
				return ErrInvalidResponse
			}
			created, e := time.Parse(time.RFC3339, node.Created)
			if e != nil {
				return ErrInvalidResponse
			}
			updated, e := time.Parse(time.RFC3339, node.UpdatedAt)
			if e != nil || updated.Before(created) {
				return ErrInvalidResponse
			}
			orderItems := make([]sdk.RemoteOrderItem, 0, len(node.Lines))
			for _, line := range node.Lines {
				// A line with no assigned variant (a custom/one-off order
				// line Saleor allows) has nothing this connector can
				// address by; skip it rather than fail the whole order.
				if line.ProductVariantID == nil || *line.ProductVariantID == "" || line.Quantity < 1 {
					continue
				}
				if !validRemoteText(line.ID, 512) {
					return ErrInvalidResponse
				}
				item := sdk.RemoteOrderItem{RemoteID: line.ID, VariantRemoteID: *line.ProductVariantID, Quantity: int64(line.Quantity)}
				if item.Validate() != nil {
					return ErrInvalidResponse
				}
				orderItems = append(orderItems, item)
			}
			item := sdk.RemoteOrder{RemoteID: node.ID, ExternalID: node.Number, StatusRemoteID: node.Status, CreatedAt: created.UTC(), UpdatedAt: updated.UTC(), Items: orderItems}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, nextErr := nextRelayCursor(result.Orders.PageInfo.HasNextPage, result.Orders.PageInfo.EndCursor, fingerprint)
		if nextErr != nil {
			return nextErr
		}
		output = sdk.OrderPage{Items: items, NextCursor: next}
		return output.Validate(request.Limit)
	})
	return output, err
}

const orderStatusQuery = `query OrderStatus($id: ID!) { order(id: $id) { id status } }`

func (connector *Connector) fetchOrderStatus(ctx context.Context, configuration Configuration, credential credentials, id string) (string, error) {
	data, err := connector.graphql(ctx, configuration, credential, orderStatusQuery, map[string]any{"id": id})
	if err != nil {
		return "", err
	}
	var result struct {
		Order *struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"order"`
	}
	if json.Unmarshal(data, &result) != nil {
		return "", ErrInvalidResponse
	}
	if result.Order == nil {
		return "", newNotFound()
	}
	if result.Order.ID != id || !validRemoteText(result.Order.Status, 64) {
		return "", ErrInvalidResponse
	}
	return result.Order.Status, nil
}

const cancelOrderQuery = `mutation CancelOrder($id: ID!) {
  orderCancel(id: $id) {
    order { id status }
    errors { field message code }
  }
}`

// WriteOrderStatus only supports canceling an order via Saleor's own
// orderCancel mutation. Every other Saleor order status (UNFULFILLED,
// PARTIALLY_FULFILLED, FULFILLED, ...) is a side effect of fulfillment/
// invoicing workflows with their own mutations, not a directly settable
// field.
func (connector *Connector) WriteOrderStatus(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.OrderStatusWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.status.write") != nil || request.Validate() != nil || request.StatusRemoteID != "CANCELED" {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		current, e := connector.fetchOrderStatus(ctx, configuration, credential, request.OrderRemoteID)
		if e == nil && current == "CANCELED" {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		var payload struct {
			OrderCancel struct {
				Errors []mutationErrorEntry `json:"errors"`
			} `json:"orderCancel"`
		}
		data, callErr := connector.graphql(ctx, configuration, credential, cancelOrderQuery, map[string]any{"id": request.OrderRemoteID})
		if callErr == nil {
			if json.Unmarshal(data, &payload) != nil {
				callErr = ErrInvalidResponse
			} else {
				callErr = mutationErr(payload.OrderCancel.Errors)
			}
		}
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			reconciled, reconcileErr := connector.fetchOrderStatus(ctx, configuration, credential, request.OrderRemoteID)
			if reconcileErr == nil && reconciled == "CANCELED" {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		updated, e := connector.fetchOrderStatus(ctx, configuration, credential, request.OrderRemoteID)
		if e != nil || updated != "CANCELED" {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}
