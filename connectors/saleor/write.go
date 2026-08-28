package saleor

import (
	"context"
	"encoding/json"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func validSaleorProductStatus(status string) bool {
	return status == "published" || status == "unpublished"
}
func desiredPublished(status string) bool { return status == "published" }

const setVariantSKUQuery = `mutation SetVariantSKU($id: ID!, $sku: String!) {
  productVariantUpdate(id: $id, input: {sku: $sku}) {
    productVariant { id sku }
    errors { field message code }
  }
}`

func (connector *Connector) setVariantSKU(ctx context.Context, configuration Configuration, credential credentials, id, sku string) error {
	var payload struct {
		ProductVariantUpdate struct {
			Errors []mutationErrorEntry `json:"errors"`
		} `json:"productVariantUpdate"`
	}
	data, err := connector.graphql(ctx, configuration, credential, setVariantSKUQuery, map[string]any{"id": id, "sku": sku})
	if err != nil {
		return err
	}
	if json.Unmarshal(data, &payload) != nil {
		return ErrInvalidResponse
	}
	return mutationErr(payload.ProductVariantUpdate.Errors)
}

const setProductNameQuery = `mutation SetProductName($id: ID!, $name: String!) {
  productUpdate(id: $id, input: {name: $name}) {
    product { id name }
    errors { field message code }
  }
}`

func (connector *Connector) setProductName(ctx context.Context, configuration Configuration, credential credentials, id, name string) error {
	var payload struct {
		ProductUpdate struct {
			Errors []mutationErrorEntry `json:"errors"`
		} `json:"productUpdate"`
	}
	data, err := connector.graphql(ctx, configuration, credential, setProductNameQuery, map[string]any{"id": id, "name": name})
	if err != nil {
		return err
	}
	if json.Unmarshal(data, &payload) != nil {
		return ErrInvalidResponse
	}
	return mutationErr(payload.ProductUpdate.Errors)
}

const setProductPublishedQuery = `mutation SetProductPublished($id: ID!, $channelId: ID!, $isPublished: Boolean!) {
  productChannelListingUpdate(id: $id, input: {updateChannels: [{channelId: $channelId, isPublished: $isPublished}]}) {
    product { id }
    errors { field message code }
  }
}`

func (connector *Connector) setProductPublished(ctx context.Context, configuration Configuration, credential credentials, id, channelID string, isPublished bool) error {
	var payload struct {
		ProductChannelListingUpdate struct {
			Errors []mutationErrorEntry `json:"errors"`
		} `json:"productChannelListingUpdate"`
	}
	data, err := connector.graphql(ctx, configuration, credential, setProductPublishedQuery, map[string]any{"id": id, "channelId": channelID, "isPublished": isPublished})
	if err != nil {
		return err
	}
	if json.Unmarshal(data, &payload) != nil {
		return ErrInvalidResponse
	}
	return mutationErr(payload.ProductChannelListingUpdate.Errors)
}

func productWriteMatches(detail variantDetail, channelSlug string, request sdk.ProductWriteRequest) bool {
	published, ok := detail.publishedIn(channelSlug)
	return detail.SKU == request.SellerSKU && detail.Product.Name == request.Title && ok && published == desiredPublished(request.StatusRemoteID)
}

// UpsertProduct only supports updating an already-admitted variant.
// Creating a new Saleor product (RemoteID == "") is deliberately
// unsupported: Saleor requires a productType assignment to create a valid
// product (ProductCreateInput.productType: ID!, which defines the
// product's whole attribute schema), which sdk.ProductWriteRequest does
// not carry -- defaulting it to some assumed product type would put a real,
// publicly visible product live with the wrong attribute set.
//
// Saleor has no single combined mutation for SKU + product name + channel
// publication the way Magento/Shopware address a whole product row with
// one PUT: SellerSKU lives on ProductVariant, Title and StatusRemoteID
// (publication) live on the shared parent Product. This connector issues
// only the mutations needed for the fields that actually changed, then
// re-fetches once to confirm the full desired state landed, matching this
// repository's fetch-compare-write-reconcile pattern even though it is not
// a single remote write. Title/StatusRemoteID changes are consequently
// shared with any sibling variants under the same parent product -- Saleor
// has no per-variant name or publication state to scope them to instead.
func (connector *Connector) UpsertProduct(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ProductWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.write") != nil || request.Validate() != nil || !validSaleorProductStatus(request.StatusRemoteID) {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	if request.RemoteID == "" {
		return sdk.CommerceWriteReceipt{}, unsupportedOperation("product_create_unsupported")
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		current, e := connector.fetchVariant(ctx, configuration, credential, request.RemoteID)
		if e != nil {
			return e
		}
		if productWriteMatches(current, configuration.Channel, request) {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.RemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		channel, e := connector.resolveChannel(ctx, configuration, credential)
		if e != nil {
			return e
		}
		ambiguous := false
		if current.SKU != request.SellerSKU {
			if callErr := connector.setVariantSKU(ctx, configuration, credential, request.RemoteID, request.SellerSKU); callErr != nil {
				if !isAmbiguousWrite(callErr) {
					return callErr
				}
				ambiguous = true
			}
		}
		if current.Product.Name != request.Title {
			if callErr := connector.setProductName(ctx, configuration, credential, current.Product.ID, request.Title); callErr != nil {
				if !isAmbiguousWrite(callErr) {
					return callErr
				}
				ambiguous = true
			}
		}
		if published, ok := current.publishedIn(configuration.Channel); !ok || published != desiredPublished(request.StatusRemoteID) {
			if callErr := connector.setProductPublished(ctx, configuration, credential, current.Product.ID, channel.ID, desiredPublished(request.StatusRemoteID)); callErr != nil {
				if !isAmbiguousWrite(callErr) {
					return callErr
				}
				ambiguous = true
			}
		}
		updated, e := connector.fetchVariant(ctx, configuration, credential, request.RemoteID)
		if e == nil && productWriteMatches(updated, configuration.Channel, request) {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.RemoteID, Applied: true, Reconciled: ambiguous}
			return receipt.Validate()
		}
		if ambiguous {
			return writeOutcomeUnknown()
		}
		return ErrInvalidResponse
	})
	return receipt, err
}

const setVariantPriceQuery = `mutation SetVariantPrice($id: ID!, $channelId: ID!, $price: PositiveDecimal!) {
  productVariantChannelListingUpdate(id: $id, input: [{channelId: $channelId, price: $price}]) {
    variant { id }
    errors { field message code }
  }
}`

func (connector *Connector) WritePrice(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PriceWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		channel, e := connector.resolveChannel(ctx, configuration, credential)
		if e != nil {
			return e
		}
		if request.Currency != channel.Currency {
			return sdk.ErrInvalidCommerceWrite
		}
		current, e := connector.fetchVariant(ctx, configuration, credential, request.VariantRemoteID)
		if e != nil {
			return e
		}
		if amount, _, ok := current.priceIn(configuration.Channel); ok && amount.String() == request.Value {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		var payload struct {
			ProductVariantChannelListingUpdate struct {
				Errors []mutationErrorEntry `json:"errors"`
			} `json:"productVariantChannelListingUpdate"`
		}
		data, callErr := connector.graphql(ctx, configuration, credential, setVariantPriceQuery, map[string]any{"id": request.VariantRemoteID, "channelId": channel.ID, "price": json.Number(request.Value)})
		if callErr == nil {
			if json.Unmarshal(data, &payload) != nil {
				callErr = ErrInvalidResponse
			} else {
				callErr = mutationErr(payload.ProductVariantChannelListingUpdate.Errors)
			}
		}
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			reconciled, reconcileErr := connector.fetchVariant(ctx, configuration, credential, request.VariantRemoteID)
			if reconcileErr == nil {
				if amount, _, ok := reconciled.priceIn(configuration.Channel); ok && amount.String() == request.Value {
					receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
					return receipt.Validate()
				}
			}
			return writeOutcomeUnknown()
		}
		updated, e := connector.fetchVariant(ctx, configuration, credential, request.VariantRemoteID)
		amount, _, ok := updated.priceIn(configuration.Channel)
		if e != nil || !ok || amount.String() != request.Value {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}
