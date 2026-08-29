package shopify

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// decimalMinorUnits/formatMinorUnits give exact (no float) two-decimal money
// arithmetic, the same technique used for the payment connectors' amount
// handling, needed here because Shopify's Refund resource carries amount
// only per-transaction (a refund can post more than one transaction) rather
// than as a single top-level field like WooCommerce's.
func decimalMinorUnits(value string) (int64, error) {
	value = strings.TrimSpace(value)
	whole, frac, hasFrac := strings.Cut(value, ".")
	if whole == "" || (hasFrac && len(frac) == 0) || len(frac) > 2 {
		return 0, ErrInvalidResponse
	}
	for _, r := range whole + frac {
		if r < '0' || r > '9' {
			return 0, ErrInvalidResponse
		}
	}
	for len(frac) < 2 {
		frac += "0"
	}
	return strconv.ParseInt(whole+frac, 10, 64)
}
func formatMinorUnits(minor int64) string {
	whole, frac := minor/100, minor%100
	fracText := strconv.FormatInt(frac, 10)
	if len(fracText) < 2 {
		fracText = "0" + fracText
	}
	return strconv.FormatInt(whole, 10) + "." + fracText
}

func refundAmount(transactions []shopifyRefundTransaction) (string, error) {
	if len(transactions) == 0 {
		return "", errors.New("shopify: refund without transactions")
	}
	var total int64
	for _, transaction := range transactions {
		minor, err := decimalMinorUnits(transaction.Amount)
		if err != nil || minor < 0 {
			return "", ErrInvalidResponse
		}
		total += minor
	}
	return formatMinorUnits(total), nil
}

func (connector *Connector) ReadReturns(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.ReturnQuery) (sdk.ReturnPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "returns.read") != nil || query.Validate(100) != nil {
		return sdk.ReturnPage{}, sdk.ErrInvalidReturnRead
	}
	orderID, err := parsePositiveID(query.OrderRemoteID)
	if err != nil {
		return sdk.ReturnPage{}, sdk.ErrInvalidReturnRead
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ReturnPage{}, err
	}
	fingerprint := configuration.fingerprint("returns:" + query.OrderRemoteID)
	pageInfo, err := decodePageCursor(query.Cursor, fingerprint)
	if err != nil {
		return sdk.ReturnPage{}, sdk.ErrInvalidReturnRead
	}
	var output sdk.ReturnPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		response, callErr := connector.call(ctx, configuration, credential, "GET", "/orders/"+intString(orderID)+"/refunds.json", listQuery(pageInfo, query.Limit), nil)
		if callErr != nil {
			return callErr
		}
		var page struct {
			Refunds []shopifyRefund `json:"refunds"`
		}
		if json.Unmarshal(response.Body, &page) != nil || len(page.Refunds) > query.Limit {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteReturn, 0, len(page.Refunds))
		for _, row := range page.Refunds {
			if row.ID < 1 {
				return ErrInvalidResponse
			}
			created, e := time.Parse(time.RFC3339, row.CreatedAt)
			if e != nil {
				return ErrInvalidResponse
			}
			amount, e := refundAmount(row.Transactions)
			if e != nil {
				return ErrInvalidResponse
			}
			item := sdk.RemoteReturn{RemoteID: intString(row.ID), OrderRemoteID: query.OrderRemoteID, Amount: amount, Currency: configuration.StoreCurrency, Reason: row.Note, CreatedAt: created.UTC()}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, e := nextCursor(response.NextPageInfo, fingerprint)
		if e != nil {
			return e
		}
		output = sdk.ReturnPage{Items: items, NextCursor: next}
		return output.Validate(query.Limit)
	})
	return output, err
}
