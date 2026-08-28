package medusa

import (
	"context"
	"encoding/json"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func (connector *Connector) ReadReturns(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.ReturnQuery) (sdk.ReturnPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "returns.read") != nil || query.Validate(100) != nil {
		return sdk.ReturnPage{}, sdk.ErrInvalidReturnRead
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ReturnPage{}, err
	}
	fingerprint := configuration.fingerprint("returns:" + query.OrderRemoteID)
	offset, err := decodePageCursor(query.Cursor, fingerprint)
	if err != nil {
		return sdk.ReturnPage{}, sdk.ErrInvalidReturnRead
	}
	var output sdk.ReturnPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		response, callErr := connector.call(ctx, configuration, credential, "GET", "/returns", []QueryParam{{Name: "order_id", Value: query.OrderRemoteID}, {Name: "offset", Value: intString(offset)}, {Name: "limit", Value: intString(query.Limit)}, {Name: "fields", Value: "id,status,order_id,refund_amount,created_at"}}, nil)
		if callErr != nil {
			return callErr
		}
		var page struct {
			Returns []medusaReturn `json:"returns"`
			Count   int            `json:"count"`
		}
		if json.Unmarshal(response.Body, &page) != nil || len(page.Returns) > query.Limit {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteReturn, 0, len(page.Returns))
		for _, row := range page.Returns {
			if row.ID == "" || row.OrderID != query.OrderRemoteID {
				return ErrInvalidResponse
			}
			created, e := time.Parse(time.RFC3339, row.CreatedAt)
			if e != nil {
				return ErrInvalidResponse
			}
			amount := row.RefundAmount.String()
			if amount == "" {
				amount = "0"
			}
			// Medusa returns have no single top-level reason string (reasons
			// live per return-item via reason_id); Reason is left empty
			// rather than mislabeling the return's lifecycle status as one.
			item := sdk.RemoteReturn{RemoteID: row.ID, OrderRemoteID: query.OrderRemoteID, Amount: amount, Currency: configuration.StoreCurrency, CreatedAt: created.UTC()}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, e := nextCursor(offset, query.Limit, page.Count, fingerprint)
		if e != nil {
			return e
		}
		output = sdk.ReturnPage{Items: items, NextCursor: next}
		return output.Validate(query.Limit)
	})
	return output, err
}
