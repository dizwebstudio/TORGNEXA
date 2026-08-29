package saleor

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func (connector *Connector) ReadPrices(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.PricePage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.read") != nil || request.Validate(50) != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.PricePage{}, err
	}
	fingerprint := configuration.fingerprint("prices")
	after, err := decodeRelayCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.PricePage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		connection, callErr := connector.listVariants(ctx, configuration, credential, request.Limit, after)
		if callErr != nil {
			return callErr
		}
		items := make([]sdk.RemotePrice, 0, len(connection.Edges))
		for _, edge := range connection.Edges {
			node := edge.Node
			if node.SKU == nil || *node.SKU == "" {
				continue
			}
			amount, currency, ok := priceInListings(node.ChannelListings, configuration.Channel)
			if !ok {
				// Not yet assigned a price in this channel: nothing to
				// report for this row rather than a fabricated zero price.
				continue
			}
			updated, parseErr := time.Parse(time.RFC3339, node.UpdatedAt)
			if parseErr != nil {
				return ErrInvalidResponse
			}
			item := sdk.RemotePrice{VariantRemoteID: node.ID, Value: amount.String(), Currency: currency, UpdatedAt: updated.UTC()}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, nextErr := nextRelayCursor(connection.PageInfo.HasNextPage, connection.PageInfo.EndCursor, fingerprint)
		if nextErr != nil {
			return nextErr
		}
		output = sdk.PricePage{Items: items, NextCursor: next}
		return output.Validate(request.Limit)
	})
	return output, err
}
