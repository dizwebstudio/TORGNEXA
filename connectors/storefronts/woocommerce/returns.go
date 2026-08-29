package woocommerce

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func (connector *Connector) ReadReturns(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.ReturnQuery) (sdk.ReturnPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "returns.read") != nil || query.Validate(100) != nil {
		return sdk.ReturnPage{}, sdk.ErrInvalidReturnRead
	}
	orderID, err := strconv.ParseInt(query.OrderRemoteID, 10, 64)
	if err != nil || orderID < 1 {
		return sdk.ReturnPage{}, sdk.ErrInvalidReturnRead
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ReturnPage{}, err
	}
	page, err := decodePageCursor(query.Cursor, configuration.fingerprint("returns:"+query.OrderRemoteID))
	if err != nil {
		return sdk.ReturnPage{}, sdk.ErrInvalidReturnRead
	}
	var output sdk.ReturnPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		response, callErr := connector.call(ctx, configuration, credential, "GET", "/orders/"+intString(orderID)+"/refunds", []QueryParam{{Name: "page", Value: intString(int64(page))}, {Name: "per_page", Value: intString(int64(query.Limit))}}, nil)
		if callErr != nil {
			return callErr
		}
		var rows []wooRefund
		if json.Unmarshal(response.Body, &rows) != nil || len(rows) > query.Limit {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteReturn, 0, len(rows))
		for _, row := range rows {
			if row.ID < 1 {
				return ErrInvalidResponse
			}
			created, e := parseWooTime(row.DateCreatedGMT)
			if e != nil {
				return e
			}
			item := sdk.RemoteReturn{RemoteID: intString(row.ID), OrderRemoteID: query.OrderRemoteID, Amount: row.Amount, Currency: configuration.StoreCurrency, Reason: row.Reason, CreatedAt: created}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, e := nextCursor(page, query.Limit, len(rows), response.TotalPages, configuration.fingerprint("returns:"+query.OrderRemoteID))
		if e != nil {
			return e
		}
		output = sdk.ReturnPage{Items: items, NextCursor: next}
		return output.Validate(query.Limit)
	})
	return output, err
}
