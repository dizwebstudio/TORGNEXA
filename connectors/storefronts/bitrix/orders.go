package bitrix

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const maxBitrixOrderBasketItems = 5000

func (connector *Connector) listSaleOrders(ctx context.Context, configuration Configuration, credential credentials, offset int) ([]bitrixSaleOrder, int, error) {
	response, err := connector.call(ctx, configuration, credential, "sale.order.list", map[string]any{
		"select": []string{"id", "accountNumber", "xmlId", "statusId", "dateInsert", "dateUpdate"},
		"order":  map[string]string{"id": "asc"},
		"start":  offset,
	})
	if err != nil {
		return nil, 0, err
	}
	return decodeSaleOrderList(response.Body)
}

func (connector *Connector) listBasketItems(ctx context.Context, configuration Configuration, credential credentials, orderIDs []int64, offset int) ([]bitrixBasketItem, int, error) {
	if len(orderIDs) == 0 || len(orderIDs) > 50 || offset < 0 {
		return nil, 0, ErrInvalidResponse
	}
	response, err := connector.call(ctx, configuration, credential, "sale.basketitem.list", map[string]any{
		"select": []string{"id", "orderId", "productId", "quantity"},
		"filter": map[string]any{"@orderId": orderIDs},
		"order":  map[string]string{"id": "asc"},
		"start":  offset,
	})
	if err != nil {
		return nil, 0, err
	}
	return decodeBasketItemList(response.Body)
}

func saleOrderItem(item bitrixBasketItem) (sdk.RemoteOrderItem, bool, error) {
	lineID := int64(item.ID)
	orderID := int64(item.OrderID)
	productID := int64(item.ProductID)
	if lineID < 1 || orderID < 1 {
		return sdk.RemoteOrderItem{}, false, ErrInvalidResponse
	}
	quantity, err := item.Quantity.Int64()
	if err != nil || quantity < 1 {
		// The canonical SDK currently supports integer quantities only. A
		// fractional remote line must fail closed instead of being rounded.
		return sdk.RemoteOrderItem{}, false, ErrInvalidResponse
	}
	if productID < 1 {
		// Bitrix allows custom basket lines with productId=0. They cannot be
		// mapped to a catalog variant, so retain the order but omit that line.
		return sdk.RemoteOrderItem{}, false, nil
	}
	line := sdk.RemoteOrderItem{RemoteID: strconv.FormatInt(lineID, 10), VariantRemoteID: strconv.FormatInt(productID, 10), Quantity: quantity}
	if line.Validate() != nil {
		return sdk.RemoteOrderItem{}, false, ErrInvalidResponse
	}
	return line, true, nil
}

func (connector *Connector) projectSaleOrder(order bitrixSaleOrder, itemsByOrder map[int64][]sdk.RemoteOrderItem) (sdk.RemoteOrder, error) {
	orderID := int64(order.ID)
	if orderID < 1 || !validRemoteText(order.StatusID, 64) || (order.AccountNumber != "" && !validRemoteText(order.AccountNumber, 300)) {
		return sdk.RemoteOrder{}, ErrInvalidResponse
	}
	createdAt, err := parseBitrixTime(order.DateInsert)
	if err != nil {
		return sdk.RemoteOrder{}, ErrInvalidResponse
	}
	updatedAt, err := parseBitrixTime(order.DateUpdate)
	if err != nil || updatedAt.Before(createdAt) {
		return sdk.RemoteOrder{}, ErrInvalidResponse
	}
	projected := sdk.RemoteOrder{
		RemoteID:       strconv.FormatInt(orderID, 10),
		ExternalID:     order.AccountNumber,
		StatusRemoteID: order.StatusID,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		Items:          itemsByOrder[orderID],
	}
	if projected.Validate() != nil {
		return sdk.RemoteOrder{}, ErrInvalidResponse
	}
	return projected, nil
}

// ReadOrders reads Bitrix sale orders and their catalog-backed basket lines.
// The Bitrix REST list surface is capped at 50 rows, so the SDK exposes that
// page size and carries the provider offset in the opaque cursor.
func (connector *Connector) ReadOrders(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.OrderPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.read") != nil || request.Limit != 50 || request.Validate(50) != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.OrderPage{}, err
	}
	offset, err := decodePageCursor(request.Cursor, configuration.fingerprint("orders"))
	if err != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.OrderPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		orders, total, listErr := connector.listSaleOrders(ctx, configuration, credential, offset)
		if listErr != nil {
			return listErr
		}
		if len(orders) > request.Limit || offset > total || total > 50_000_000 {
			return ErrInvalidResponse
		}
		orderIDs := make([]int64, 0, len(orders))
		seenOrders := make(map[int64]struct{}, len(orders))
		for _, order := range orders {
			id := int64(order.ID)
			if id < 1 {
				return ErrInvalidResponse
			}
			if _, duplicate := seenOrders[id]; duplicate {
				return ErrInvalidResponse
			}
			seenOrders[id] = struct{}{}
			orderIDs = append(orderIDs, id)
		}

		itemsByOrder := make(map[int64][]sdk.RemoteOrderItem, len(orderIDs))
		seenLines := make(map[int64]struct{})
		for basketOffset := 0; len(orderIDs) > 0; {
			basketItems, basketTotal, basketErr := connector.listBasketItems(ctx, configuration, credential, orderIDs, basketOffset)
			if basketErr != nil {
				return basketErr
			}
			if basketTotal > maxBitrixOrderBasketItems || basketOffset > basketTotal {
				return ErrInvalidResponse
			}
			for _, item := range basketItems {
				orderID := int64(item.OrderID)
				if _, known := seenOrders[orderID]; !known {
					return ErrInvalidResponse
				}
				lineID := int64(item.ID)
				if _, duplicate := seenLines[lineID]; duplicate {
					return ErrInvalidResponse
				}
				seenLines[lineID] = struct{}{}
				line, include, lineErr := saleOrderItem(item)
				if lineErr != nil {
					return lineErr
				}
				if include {
					itemsByOrder[orderID] = append(itemsByOrder[orderID], line)
				}
			}
			if len(basketItems) < 50 || basketOffset+len(basketItems) >= basketTotal {
				break
			}
			basketOffset += len(basketItems)
		}

		items := make([]sdk.RemoteOrder, 0, len(orders))
		for _, order := range orders {
			projected, projectErr := connector.projectSaleOrder(order, itemsByOrder)
			if projectErr != nil {
				return projectErr
			}
			items = append(items, projected)
		}
		if len(orders) == 50 && offset+len(orders) < total {
			next, cursorErr := encodePageCursor(offset+len(orders), configuration.fingerprint("orders"))
			if cursorErr != nil {
				return cursorErr
			}
			output.NextCursor = next
		}
		output.Items = items
		return output.Validate(request.Limit)
	})
	return output, err
}

func (connector *Connector) fetchSaleOrderStatus(ctx context.Context, configuration Configuration, credential credentials, orderID int64) (string, error) {
	response, err := connector.call(ctx, configuration, credential, "sale.order.get", map[string]any{"id": orderID})
	if err != nil {
		return "", err
	}
	var envelope struct {
		Result *struct {
			Order bitrixSaleOrder `json:"order"`
		} `json:"result"`
	}
	if json.Unmarshal(response.Body, &envelope) != nil || envelope.Result == nil || int64(envelope.Result.Order.ID) != orderID || !validRemoteText(envelope.Result.Order.StatusID, 64) {
		return "", ErrInvalidResponse
	}
	return envelope.Result.Order.StatusID, nil
}

// WriteOrderStatus updates the provider-native Bitrix sale order status. The
// REST update has no portable idempotency field, so retries are protected by
// read-before/read-after reconciliation and never blindly repeated after an
// ambiguous transport result.
func (connector *Connector) WriteOrderStatus(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.OrderStatusWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.status.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	orderID, err := strconv.ParseInt(request.OrderRemoteID, 10, 64)
	if err != nil || orderID < 1 {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		current, statusErr := connector.fetchSaleOrderStatus(ctx, configuration, credential, orderID)
		if statusErr != nil {
			return statusErr
		}
		if current == request.StatusRemoteID {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}

		_, updateErr := connector.call(ctx, configuration, credential, "sale.order.update", map[string]any{
			"id":     orderID,
			"fields": map[string]any{"statusId": request.StatusRemoteID},
		})
		if updateErr != nil && !isAmbiguousWrite(updateErr) {
			return updateErr
		}
		verified, verifyErr := connector.fetchSaleOrderStatus(ctx, configuration, credential, orderID)
		if verifyErr == nil && verified == request.StatusRemoteID {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Applied: updateErr == nil, Duplicate: updateErr != nil, Reconciled: updateErr != nil}
			return receipt.Validate()
		}
		if updateErr != nil {
			return writeOutcomeUnknown()
		}
		if verifyErr != nil {
			return verifyErr
		}
		return ErrInvalidResponse
	})
	return receipt, err
}
