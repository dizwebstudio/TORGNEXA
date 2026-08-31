package cscart

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type csCartOrder struct {
	OrderID   json.RawMessage               `json:"order_id"`
	Timestamp json.RawMessage               `json:"timestamp"`
	Status    string                        `json:"status"`
	Products  map[string]csCartOrderProduct `json:"products"`
}

type csCartOrderProduct struct {
	ProductID json.RawMessage            `json:"product_id"`
	Amount    json.RawMessage            `json:"amount"`
	Options   map[string]json.RawMessage `json:"product_options"`
}

type orderListResponse struct {
	Orders []csCartOrder `json:"orders"`
	Params struct {
		TotalItems json.RawMessage `json:"total_items"`
	} `json:"params"`
}

func (c *Connector) listOrders(ctx context.Context, cfg Configuration, cred credentials, page, limit int) ([]csCartOrder, int, error) {
	query := []QueryParam{
		{Name: "page", Value: strconv.Itoa(page)},
		{Name: "items_per_page", Value: strconv.Itoa(limit)},
		{Name: "sort_by", Value: "date"},
		{Name: "sort_order", Value: "asc"},
	}
	resp, err := c.call(ctx, cfg, cred, "GET", "/orders", query, nil)
	if err != nil {
		return nil, 0, err
	}
	var envelope orderListResponse
	if json.Unmarshal(resp.Body, &envelope) != nil || len(envelope.Orders) > limit {
		return nil, 0, ErrInvalidResponse
	}
	total := 0
	if len(envelope.Params.TotalItems) > 0 {
		value, parseErr := rawString(envelope.Params.TotalItems)
		if parseErr != nil {
			return nil, 0, ErrInvalidResponse
		}
		total, parseErr = strconv.Atoi(value)
		if parseErr != nil || total < 0 {
			return nil, 0, ErrInvalidResponse
		}
	}
	return envelope.Orders, total, nil
}

func (c *Connector) fetchOrder(ctx context.Context, cfg Configuration, cred credentials, remoteID string) (csCartOrder, error) {
	parsed, err := strconv.ParseInt(remoteID, 10, 64)
	if err != nil || parsed < 1 {
		return csCartOrder{}, ErrInvalidResponse
	}
	resp, err := c.call(ctx, cfg, cred, "GET", "/orders/"+remoteID, nil, nil)
	if err != nil {
		return csCartOrder{}, err
	}
	var order csCartOrder
	if json.Unmarshal(resp.Body, &order) != nil {
		return csCartOrder{}, ErrInvalidResponse
	}
	id, err := rawString(order.OrderID)
	if err != nil || id != remoteID {
		return csCartOrder{}, ErrInvalidResponse
	}
	return order, nil
}

func orderTimestamp(order csCartOrder) (int64, error) {
	value, err := rawString(order.Timestamp)
	if err != nil {
		return 0, ErrInvalidResponse
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds < 1 {
		return 0, ErrInvalidResponse
	}
	return seconds, nil
}

func orderItems(order csCartOrder) ([]sdk.RemoteOrderItem, error) {
	if len(order.Products) == 0 || len(order.Products) > 1000 {
		return nil, ErrInvalidResponse
	}
	keys := make([]string, 0, len(order.Products))
	for key := range order.Products {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]sdk.RemoteOrderItem, 0, len(keys))
	for _, lineID := range keys {
		line := order.Products[lineID]
		if len(line.Options) != 0 {
			return nil, ErrInvalidResponse
		}
		productID, err := rawString(line.ProductID)
		if err != nil {
			return nil, ErrInvalidResponse
		}
		parsedProductID, err := strconv.ParseInt(productID, 10, 64)
		if err != nil || parsedProductID < 1 {
			return nil, ErrInvalidResponse
		}
		amount, err := rawString(line.Amount)
		if err != nil {
			return nil, ErrInvalidResponse
		}
		quantity, err := strconv.ParseInt(amount, 10, 64)
		if err != nil || quantity < 1 {
			return nil, ErrInvalidResponse
		}
		item := sdk.RemoteOrderItem{RemoteID: lineID, VariantRemoteID: productID, Quantity: quantity}
		if item.Validate() != nil {
			return nil, ErrInvalidResponse
		}
		items = append(items, item)
	}
	return items, nil
}

// ReadOrders reads CS-Cart orders and their catalog-backed lines. The API
// exposes placement time rather than a separate update time, so the neutral
// projection uses the placement timestamp for both order timestamps.
func (c *Connector) ReadOrders(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.OrderPage, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.read") != nil || request.Validate(100) != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.OrderPage{}, err
	}
	fingerprint := cfg.fingerprint("orders")
	page, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.OrderPage
	err = c.withCredentials(ctx, runtime, account.SecretReference, func(cred credentials) error {
		orders, total, listErr := c.listOrders(ctx, cfg, cred, page, request.Limit)
		if listErr != nil {
			return listErr
		}
		items := make([]sdk.RemoteOrder, 0, len(orders))
		for _, summary := range orders {
			id, idErr := rawString(summary.OrderID)
			if idErr != nil {
				return ErrInvalidResponse
			}
			order, fetchErr := c.fetchOrder(ctx, cfg, cred, id)
			if fetchErr != nil {
				return fetchErr
			}
			if !validRemoteText(order.Status, 64) {
				return ErrInvalidResponse
			}
			seconds, timeErr := orderTimestamp(order)
			if timeErr != nil {
				return timeErr
			}
			lines, linesErr := orderItems(order)
			if linesErr != nil {
				return linesErr
			}
			created := time.Unix(seconds, 0).UTC()
			projected := sdk.RemoteOrder{RemoteID: id, StatusRemoteID: order.Status, CreatedAt: created, UpdatedAt: created, Items: lines}
			if projected.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, projected)
		}
		if len(orders) == request.Limit && (total == 0 || page*request.Limit < total) {
			cursor, cursorErr := encodePageCursor(page+1, fingerprint)
			if cursorErr != nil {
				return cursorErr
			}
			output.NextCursor = cursor
		}
		output.Items = items
		return output.Validate(request.Limit)
	})
	return output, err
}

var writableOrderStatuses = map[string]struct{}{
	"O": {}, // open
	"Y": {}, // awaiting call
	"P": {}, // processed
	"B": {}, // backordered
	"C": {}, // complete
	"I": {}, // cancelled
	"F": {}, // failed
	"D": {}, // declined
}

func (c *Connector) fetchOrderStatus(ctx context.Context, cfg Configuration, cred credentials, remoteID string) (string, error) {
	order, err := c.fetchOrder(ctx, cfg, cred, remoteID)
	if err != nil {
		return "", err
	}
	if !validRemoteText(order.Status, 1) {
		return "", ErrInvalidResponse
	}
	if _, ok := writableOrderStatuses[order.Status]; !ok {
		return "", ErrInvalidResponse
	}
	return order.Status, nil
}

// WriteOrderStatus updates one of CS-Cart's standard order status codes. The
// API accepts a partial order PUT; the adapter still reads the current status
// first and verifies it after the write so a timeout cannot be retried blindly.
func (c *Connector) WriteOrderStatus(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.OrderStatusWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.status.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	if _, ok := writableOrderStatuses[request.StatusRemoteID]; !ok {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	if parsed, err := strconv.ParseInt(request.OrderRemoteID, 10, 64); err != nil || parsed < 1 {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = c.withCredentials(ctx, runtime, account.SecretReference, func(cred credentials) error {
		current, readErr := c.fetchOrderStatus(ctx, cfg, cred, request.OrderRemoteID)
		if readErr != nil {
			return readErr
		}
		if current == request.StatusRemoteID {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, marshalErr := json.Marshal(struct {
			Status string `json:"status"`
		}{Status: request.StatusRemoteID})
		if marshalErr != nil {
			return marshalErr
		}
		_, callErr := c.call(ctx, cfg, cred, "PUT", "/orders/"+request.OrderRemoteID, nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			verified, verifyErr := c.fetchOrderStatus(ctx, cfg, cred, request.OrderRemoteID)
			if verifyErr == nil && verified == request.StatusRemoteID {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		verified, verifyErr := c.fetchOrderStatus(ctx, cfg, cred, request.OrderRemoteID)
		if verifyErr != nil || verified != request.StatusRemoteID {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Applied: true, Reconciled: true}
		return receipt.Validate()
	})
	return receipt, err
}
