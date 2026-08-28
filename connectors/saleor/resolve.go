package saleor

import (
	"context"
	"encoding/json"
)

type resolvedChannel struct {
	ID       string
	Currency string
}

const channelBySlugQuery = `query ChannelBySlug($slug: String!) { channel(slug: $slug) { id currencyCode } }`

// resolveChannel maps the admin-supplied channel slug to Saleor's own
// channel id and currency, mirroring Shopware's currency-UUID resolution:
// every write mutation that targets a channel (price, publication status)
// takes a channel id, not a slug, and the channel's currencyCode is the
// only source of truth for validating an incoming PriceWriteRequest's
// currency, since productVariantChannelListingUpdate accepts a bare decimal
// amount with no currency field of its own.
func (connector *Connector) resolveChannel(ctx context.Context, configuration Configuration, credential credentials) (resolvedChannel, error) {
	connector.mu.Lock()
	cached, ok := connector.channels[configuration.Channel]
	connector.mu.Unlock()
	if ok {
		return cached, nil
	}
	data, err := connector.graphql(ctx, configuration, credential, channelBySlugQuery, map[string]any{"slug": configuration.Channel})
	if err != nil {
		return resolvedChannel{}, err
	}
	var result struct {
		Channel *struct {
			ID           string `json:"id"`
			CurrencyCode string `json:"currencyCode"`
		} `json:"channel"`
	}
	if json.Unmarshal(data, &result) != nil || result.Channel == nil || !validRemoteText(result.Channel.ID, 512) || len(result.Channel.CurrencyCode) != 3 {
		return resolvedChannel{}, newNotFound()
	}
	resolved := resolvedChannel{ID: result.Channel.ID, Currency: result.Channel.CurrencyCode}
	connector.mu.Lock()
	connector.channels[configuration.Channel] = resolved
	connector.mu.Unlock()
	return resolved, nil
}

const warehouseBySlugQuery = `query WarehouseBySlug($slugs: [String!]) { warehouses(filter: {slugs: $slugs}, first: 1) { edges { node { id slug } } } }`

// resolveWarehouse maps the admin-supplied single-location warehouse slug to
// Saleor's own warehouse id, required by productVariantStocksUpdate's
// StockInput.warehouse field. This connector, like Magento's synthetic
// single-location model, addresses exactly one configured warehouse rather
// than modeling Saleor's full multi-warehouse allocation graph.
func (connector *Connector) resolveWarehouse(ctx context.Context, configuration Configuration, credential credentials) (string, error) {
	connector.mu.Lock()
	cached, ok := connector.warehouses[configuration.Warehouse]
	connector.mu.Unlock()
	if ok {
		return cached, nil
	}
	data, err := connector.graphql(ctx, configuration, credential, warehouseBySlugQuery, map[string]any{"slugs": []string{configuration.Warehouse}})
	if err != nil {
		return "", err
	}
	var result struct {
		Warehouses struct {
			Edges []struct {
				Node struct {
					ID   string `json:"id"`
					Slug string `json:"slug"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"warehouses"`
	}
	if json.Unmarshal(data, &result) != nil || len(result.Warehouses.Edges) != 1 {
		return "", newNotFound()
	}
	node := result.Warehouses.Edges[0].Node
	if !validRemoteText(node.ID, 512) || node.Slug != configuration.Warehouse {
		return "", ErrInvalidResponse
	}
	connector.mu.Lock()
	connector.warehouses[configuration.Warehouse] = node.ID
	connector.mu.Unlock()
	return node.ID, nil
}
