package saleor

import (
	"context"
	"encoding/json"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const listVariantsQuery = `query ListVariants($channel: String!, $first: Int!, $after: String) {
  productVariants(channel: $channel, first: $first, after: $after) {
    edges {
      cursor
      node {
        id sku updatedAt
        product { id name }
        channelListings { channel { slug } price { amount currency } }
      }
    }
    pageInfo { hasNextPage endCursor }
  }
}`

type variantNode struct {
	ID        string  `json:"id"`
	SKU       *string `json:"sku"`
	UpdatedAt string  `json:"updatedAt"`
	Product   struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"product"`
	ChannelListings []variantChannelListing `json:"channelListings"`
}

type variantEdge struct {
	Cursor string      `json:"cursor"`
	Node   variantNode `json:"node"`
}

type variantConnection struct {
	Edges    []variantEdge `json:"edges"`
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
}

func (connector *Connector) listVariants(ctx context.Context, configuration Configuration, credential credentials, first int, after string) (variantConnection, error) {
	variables := map[string]any{"channel": configuration.Channel, "first": first}
	if after != "" {
		variables["after"] = after
	}
	data, err := connector.graphql(ctx, configuration, credential, listVariantsQuery, variables)
	if err != nil {
		return variantConnection{}, err
	}
	var result struct {
		ProductVariants variantConnection `json:"productVariants"`
	}
	if json.Unmarshal(data, &result) != nil || len(result.ProductVariants.Edges) > first {
		return variantConnection{}, ErrInvalidResponse
	}
	return result.ProductVariants, nil
}

// ReadProducts walks Saleor's productVariants connection directly rather
// than the products connection: every ProductVariant.sku is projected as
// its own single-variant sdk.RemoteProduct row (RemoteID/SellerSKU come
// from the variant, Title from the shared parent Product.name), the same
// flattened simplification WooCommerce/Shopware/Magento already make for
// their own configurable/parent-child variant models. A variant with no SKU
// (Saleor's sku field is nullable) is skipped rather than failing the whole
// page, since it has nothing this connector can address it by.
func (connector *Connector) ReadProducts(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ProductPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || request.Validate(50) != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ProductPage{}, err
	}
	fingerprint := configuration.fingerprint("products")
	after, err := decodeRelayCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.ProductPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		connection, callErr := connector.listVariants(ctx, configuration, credential, request.Limit, after)
		if callErr != nil {
			return callErr
		}
		result := make([]sdk.RemoteProduct, 0, len(connection.Edges))
		for _, edge := range connection.Edges {
			node := edge.Node
			if node.SKU == nil || *node.SKU == "" {
				continue
			}
			if !validRemoteText(*node.SKU, 200) || !validRemoteText(node.Product.Name, 500) {
				return ErrInvalidResponse
			}
			updated, parseErr := time.Parse(time.RFC3339, node.UpdatedAt)
			if parseErr != nil {
				return ErrInvalidResponse
			}
			variant := sdk.RemoteVariant{RemoteID: node.ID, SKUs: []string{*node.SKU}}
			item := sdk.RemoteProduct{RemoteID: node.ID, SellerSKU: *node.SKU, Title: node.Product.Name, UpdatedAt: updated.UTC(), Variants: []sdk.RemoteVariant{variant}}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			result = append(result, item)
		}
		next, nextErr := nextRelayCursor(connection.PageInfo.HasNextPage, connection.PageInfo.EndCursor, fingerprint)
		if nextErr != nil {
			return nextErr
		}
		output = sdk.ProductPage{Items: result, NextCursor: next}
		return output.Validate(request.Limit)
	})
	return output, err
}
