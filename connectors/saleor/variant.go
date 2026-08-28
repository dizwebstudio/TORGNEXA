package saleor

import (
	"context"
	"encoding/json"
)

const variantByIDQuery = `query VariantByID($id: ID!, $channel: String!) {
  productVariant(id: $id, channel: $channel) {
    id
    sku
    product { id name channelListings { channel { slug } isPublished } }
    channelListings { channel { slug } price { amount currency } }
    stocks { warehouse { slug } quantity }
  }
}`

type variantMoney struct {
	Amount   json.Number `json:"amount"`
	Currency string      `json:"currency"`
}

type variantChannelListing struct {
	Channel struct {
		Slug string `json:"slug"`
	} `json:"channel"`
	Price *variantMoney `json:"price"`
}

type variantStock struct {
	Warehouse struct {
		Slug string `json:"slug"`
	} `json:"warehouse"`
	Quantity int `json:"quantity"`
}

type productPublicationListing struct {
	Channel struct {
		Slug string `json:"slug"`
	} `json:"channel"`
	IsPublished bool `json:"isPublished"`
}

type variantDetail struct {
	ID      string `json:"id"`
	SKU     string `json:"sku"`
	Product struct {
		ID              string                      `json:"id"`
		Name            string                      `json:"name"`
		ChannelListings []productPublicationListing `json:"channelListings"`
	} `json:"product"`
	ChannelListings []variantChannelListing `json:"channelListings"`
	Stocks          []variantStock          `json:"stocks"`
}

func (detail variantDetail) publishedIn(channelSlug string) (bool, bool) {
	for _, listing := range detail.Product.ChannelListings {
		if listing.Channel.Slug == channelSlug {
			return listing.IsPublished, true
		}
	}
	return false, false
}

// fetchVariant is the shared read-before-write lookup used by write.go,
// prices.go and inventory.go: Saleor's productVariant query already returns
// SKU, parent product name, per-channel price and per-warehouse stock in
// one round trip, so every write path's fetch-compare-write-reconcile check
// (see docs/connectors/saleor/reconciliation.md) shares this single query
// instead of one bespoke request per field.
func (connector *Connector) fetchVariant(ctx context.Context, configuration Configuration, credential credentials, id string) (variantDetail, error) {
	data, err := connector.graphql(ctx, configuration, credential, variantByIDQuery, map[string]any{"id": id, "channel": configuration.Channel})
	if err != nil {
		return variantDetail{}, err
	}
	var result struct {
		ProductVariant *variantDetail `json:"productVariant"`
	}
	if json.Unmarshal(data, &result) != nil {
		return variantDetail{}, ErrInvalidResponse
	}
	if result.ProductVariant == nil {
		return variantDetail{}, newNotFound()
	}
	if !validRemoteText(result.ProductVariant.ID, 512) || result.ProductVariant.ID != id {
		return variantDetail{}, ErrInvalidResponse
	}
	return *result.ProductVariant, nil
}

func priceInListings(listings []variantChannelListing, channelSlug string) (json.Number, string, bool) {
	for _, listing := range listings {
		if listing.Channel.Slug == channelSlug && listing.Price != nil {
			return listing.Price.Amount, listing.Price.Currency, true
		}
	}
	return "", "", false
}

func (detail variantDetail) priceIn(channelSlug string) (json.Number, string, bool) {
	return priceInListings(detail.ChannelListings, channelSlug)
}

func (detail variantDetail) stockIn(warehouseSlug string) (int, bool) {
	for _, stock := range detail.Stocks {
		if stock.Warehouse.Slug == warehouseSlug {
			return stock.Quantity, true
		}
	}
	return 0, false
}
