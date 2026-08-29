package saleor

import (
	"context"
	"encoding/json"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const orderGrantedRefundsQuery = `query OrderGrantedRefunds($id: ID!) {
  order(id: $id) {
    id
    grantedRefunds { id createdAt amount { amount currency } reason }
  }
}`

type grantedRefundNode struct {
	ID        string       `json:"id"`
	CreatedAt string       `json:"createdAt"`
	Amount    variantMoney `json:"amount"`
	Reason    string       `json:"reason"`
}

// ReadReturns paginates Order.grantedRefunds -- a plain list field with no
// first/after arguments of its own, unlike every *CountableConnection field
// this connector otherwise walks -- by fetching it in full once and then
// windowing the slice locally via an integer-offset cursor (cursor.go). A
// granted refund carries a real "reason" field (unlike Medusa's and
// Magento's own return/creditmemo shapes, which have none), so this
// connector can honestly populate sdk.RemoteReturn.Reason instead of
// leaving it blank.
func (connector *Connector) ReadReturns(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.ReturnQuery) (sdk.ReturnPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "returns.read") != nil || query.Validate(100) != nil {
		return sdk.ReturnPage{}, sdk.ErrInvalidReturnRead
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ReturnPage{}, err
	}
	fingerprint := configuration.fingerprint("returns:" + query.OrderRemoteID)
	offset, err := decodeOffsetCursor(query.Cursor, fingerprint)
	if err != nil {
		return sdk.ReturnPage{}, sdk.ErrInvalidReturnRead
	}
	var output sdk.ReturnPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		data, callErr := connector.graphql(ctx, configuration, credential, orderGrantedRefundsQuery, map[string]any{"id": query.OrderRemoteID})
		if callErr != nil {
			return callErr
		}
		var result struct {
			Order *struct {
				ID             string              `json:"id"`
				GrantedRefunds []grantedRefundNode `json:"grantedRefunds"`
			} `json:"order"`
		}
		if json.Unmarshal(data, &result) != nil {
			return ErrInvalidResponse
		}
		if result.Order == nil {
			return newNotFound()
		}
		if result.Order.ID != query.OrderRemoteID || len(result.Order.GrantedRefunds) > 5000 {
			return ErrInvalidResponse
		}
		all := result.Order.GrantedRefunds
		if offset > len(all) {
			offset = len(all)
		}
		end := offset + query.Limit
		if end > len(all) {
			end = len(all)
		}
		page := all[offset:end]
		items := make([]sdk.RemoteReturn, 0, len(page))
		for _, row := range page {
			if !validRemoteText(row.ID, 512) {
				return ErrInvalidResponse
			}
			created, e := time.Parse(time.RFC3339, row.CreatedAt)
			if e != nil {
				return ErrInvalidResponse
			}
			item := sdk.RemoteReturn{RemoteID: row.ID, OrderRemoteID: query.OrderRemoteID, Amount: row.Amount.Amount.String(), Currency: row.Amount.Currency, Reason: row.Reason, CreatedAt: created.UTC()}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, nextErr := nextOffsetCursor(offset, query.Limit, len(all), fingerprint)
		if nextErr != nil {
			return nextErr
		}
		output = sdk.ReturnPage{Items: items, NextCursor: next}
		return output.Validate(query.Limit)
	})
	return output, err
}
