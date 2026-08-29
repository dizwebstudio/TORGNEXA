package shopware

import (
	"context"
	"encoding/json"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// ReadReturns projects Shopware's order_transaction_capture_refund entity:
// Shopware's core Admin API has no first-class "Return"/RMA resource the
// way Shopify/Medusa do, but a refund against a payment capture is the
// closest equivalent — it carries an amount, an optional reason and a
// creation timestamp, linked to the order via a two-hop association path
// (refund -> transactionCapture -> transaction -> orderId), a standard
// Shopware Criteria nested-field filter.
func (connector *Connector) ReadReturns(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.ReturnQuery) (sdk.ReturnPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "returns.read") != nil || query.Validate(100) != nil {
		return sdk.ReturnPage{}, sdk.ErrInvalidReturnRead
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ReturnPage{}, err
	}
	fingerprint := configuration.fingerprint("returns:" + query.OrderRemoteID)
	page, err := decodePageCursor(query.Cursor, fingerprint)
	if err != nil {
		return sdk.ReturnPage{}, sdk.ErrInvalidReturnRead
	}
	var output sdk.ReturnPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		body, marshalErr := json.Marshal(map[string]any{
			"page": page, "limit": query.Limit,
			"filter": []map[string]any{{"type": "equals", "field": "transactionCapture.transaction.orderId", "value": query.OrderRemoteID}},
		})
		if marshalErr != nil {
			return marshalErr
		}
		response, callErr := connector.call(ctx, configuration, account.ID, credential, "POST", "/search/order-transaction-capture-refund", nil, body)
		if callErr != nil {
			return callErr
		}
		var result shopwareSearchPage[shopwareRefund]
		if json.Unmarshal(response.Body, &result) != nil || len(result.Data) > query.Limit {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteReturn, 0, len(result.Data))
		for _, row := range result.Data {
			if row.ID == "" {
				return ErrInvalidResponse
			}
			created, e := time.Parse(time.RFC3339, row.CreatedAt)
			if e != nil {
				return ErrInvalidResponse
			}
			item := sdk.RemoteReturn{RemoteID: row.ID, OrderRemoteID: query.OrderRemoteID, Amount: row.Amount.TotalPrice.String(), Currency: configuration.StoreCurrency, Reason: row.Reason, CreatedAt: created.UTC()}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, e := nextCursor(page, query.Limit, result.Total, fingerprint)
		if e != nil {
			return e
		}
		output = sdk.ReturnPage{Items: items, NextCursor: next}
		return output.Validate(query.Limit)
	})
	return output, err
}
