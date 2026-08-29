package magento

import (
	"context"
	"encoding/json"

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
	page, err := decodePageCursor(query.Cursor, fingerprint)
	if err != nil {
		return sdk.ReturnPage{}, sdk.ErrInvalidReturnRead
	}
	var output sdk.ReturnPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		filters := []searchFilter{{Field: "order_id", Value: query.OrderRemoteID}}
		queryParams := searchCriteriaQuery(page, query.Limit, filters)
		response, callErr := connector.call(ctx, configuration, credential, "GET", "/creditmemos", queryParams, nil)
		if callErr != nil {
			return callErr
		}
		var result struct {
			Items      []magentoCreditmemo `json:"items"`
			TotalCount int                 `json:"total_count"`
		}
		if json.Unmarshal(response.Body, &result) != nil || len(result.Items) > query.Limit {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteReturn, 0, len(result.Items))
		for _, row := range result.Items {
			remoteID := row.EntityID.String()
			if remoteID == "" || row.OrderID.String() != query.OrderRemoteID {
				return ErrInvalidResponse
			}
			created, e := parseMagentoTime(row.CreatedAt)
			if e != nil {
				return ErrInvalidResponse
			}
			item := sdk.RemoteReturn{RemoteID: remoteID, OrderRemoteID: query.OrderRemoteID, Amount: row.GrandTotal.String(), Currency: configuration.StoreCurrency, CreatedAt: created.UTC()}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, e := nextCursor(page, query.Limit, result.TotalCount, fingerprint)
		if e != nil {
			return e
		}
		output = sdk.ReturnPage{Items: items, NextCursor: next}
		return output.Validate(query.Limit)
	})
	return output, err
}
