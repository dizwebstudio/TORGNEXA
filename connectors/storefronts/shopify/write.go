package shopify

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func isAmbiguousWrite(err error) bool {
	if err == nil {
		return false
	}
	var remote *sdk.RemoteError
	if errors.As(err, &remote) {
		return remote.Category == sdk.ErrorUnavailable || remote.Category == sdk.ErrorTimeout || remote.Category == sdk.ErrorTransient
	}
	return false
}

func validShopifyProductStatus(status string) bool {
	switch status {
	case "active", "archived", "draft":
		return true
	default:
		return false
	}
}

func (connector *Connector) fetchProduct(ctx context.Context, configuration Configuration, credential credentials, productID int64) (shopifyProduct, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/products/"+intString(productID)+".json", nil, nil)
	if err != nil {
		return shopifyProduct{}, err
	}
	var value struct {
		Product shopifyProduct `json:"product"`
	}
	if json.Unmarshal(response.Body, &value) != nil || value.Product.ID != productID || len(value.Product.Variants) == 0 {
		return shopifyProduct{}, ErrInvalidResponse
	}
	return value.Product, nil
}

func productMatches(product shopifyProduct, request sdk.ProductWriteRequest) bool {
	return product.ID > 0 && len(product.Variants) > 0 && product.Title == request.Title && product.BodyHTML == request.Description && product.Status == request.StatusRemoteID
}

func productHasSKU(product shopifyProduct, sku string) bool {
	for _, variant := range product.Variants {
		if variant.SKU == sku {
			return true
		}
	}
	return false
}

const findShopifyVariantBySKUGraphQL = `query FindProductVariantBySKU($query: String!) {
  productVariants(first: 250, query: $query) {
    nodes {
      id
      sku
      product { id }
    }
    pageInfo { hasNextPage }
  }
}`

type shopifyGraphQLError struct {
	Message string `json:"message"`
}

type shopifyVariantBySKUResponse struct {
	Errors []shopifyGraphQLError `json:"errors"`
	Data   struct {
		ProductVariants struct {
			Nodes []struct {
				ID      string `json:"id"`
				SKU     string `json:"sku"`
				Product struct {
					ID string `json:"id"`
				} `json:"product"`
			} `json:"nodes"`
			PageInfo struct {
				HasNextPage bool `json:"hasNextPage"`
			} `json:"pageInfo"`
		} `json:"productVariants"`
	} `json:"data"`
}

func shopifyProductIDFromGID(value string) (int64, error) {
	const prefix = "gid://shopify/Product/"
	if !strings.HasPrefix(value, prefix) {
		return 0, ErrInvalidResponse
	}
	return parsePositiveID(strings.TrimPrefix(value, prefix))
}

// findProductBySKU uses Shopify's exact variant search instead of scanning an
// unbounded REST catalog. A second page is rejected: the caller must never
// choose one of multiple remote variants with the same SKU.
func (connector *Connector) findProductBySKU(ctx context.Context, configuration Configuration, credential credentials, sku string) (int64, error) {
	payload, err := json.Marshal(struct {
		Query     string `json:"query"`
		Variables struct {
			Query string `json:"query"`
		} `json:"variables"`
	}{
		Query: findShopifyVariantBySKUGraphQL,
		Variables: struct {
			Query string `json:"query"`
		}{Query: "sku:" + strconv.Quote(sku)},
	})
	if err != nil {
		return 0, ErrInvalidResponse
	}
	response, err := connector.call(ctx, configuration, credential, "POST", "/graphql.json", nil, payload)
	if err != nil {
		return 0, err
	}
	var value shopifyVariantBySKUResponse
	if json.Unmarshal(response.Body, &value) != nil || len(value.Errors) > 0 || value.Data.ProductVariants.PageInfo.HasNextPage {
		return 0, ErrInvalidResponse
	}
	var productID int64
	for _, node := range value.Data.ProductVariants.Nodes {
		if node.SKU != sku {
			continue
		}
		candidate, parseErr := shopifyProductIDFromGID(node.Product.ID)
		if parseErr != nil || node.ID == "" {
			return 0, ErrInvalidResponse
		}
		if productID != 0 && productID != candidate {
			return 0, ErrInvalidResponse
		}
		productID = candidate
	}
	return productID, nil
}

// UpsertProduct supports exact-SKU create and update. Shopify's GraphQL Admin
// search provides the idempotency lookup; the REST product endpoint performs
// the mutation because the current product contract already maps to it.
// Changing SellerSKU on an existing product remains unsupported: the SKU lives
// on a variant sub-resource and a second mutation would weaken the
// read-after-write guarantee of this generic product operation.
func (connector *Connector) UpsertProduct(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ProductWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.write") != nil || request.Validate() != nil || !validShopifyProductStatus(request.StatusRemoteID) {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		productID := int64(0)
		created := false
		if request.RemoteID != "" {
			var parseErr error
			productID, parseErr = parsePositiveID(request.RemoteID)
			if parseErr != nil {
				return sdk.ErrInvalidCommerceWrite
			}
		} else {
			var findErr error
			productID, findErr = connector.findProductBySKU(ctx, configuration, credential, request.SellerSKU)
			if findErr != nil {
				return findErr
			}
			if productID == 0 {
				body, marshalErr := json.Marshal(map[string]any{"product": map[string]any{
					"title": request.Title, "body_html": request.Description, "status": request.StatusRemoteID,
					"variants": []map[string]string{{"sku": request.SellerSKU}},
				}})
				if marshalErr != nil {
					return ErrInvalidResponse
				}
				response, callErr := connector.call(ctx, configuration, credential, "POST", "/products.json", nil, body)
				if callErr != nil {
					if !isAmbiguousWrite(callErr) {
						return callErr
					}
					productID, findErr = connector.findProductBySKU(ctx, configuration, credential, request.SellerSKU)
					if findErr != nil || productID == 0 {
						return writeOutcomeUnknown()
					}
					current, reconcileErr := connector.fetchProduct(ctx, configuration, credential, productID)
					if reconcileErr != nil || !productHasSKU(current, request.SellerSKU) || !productMatches(current, request) {
						return writeOutcomeUnknown()
					}
					receipt = sdk.CommerceWriteReceipt{RemoteID: intString(productID), Applied: true, Reconciled: true}
					return receipt.Validate()
				}
				var createdResponse struct {
					Product shopifyProduct `json:"product"`
				}
				if json.Unmarshal(response.Body, &createdResponse) != nil || createdResponse.Product.ID < 1 {
					return ErrInvalidResponse
				}
				productID = createdResponse.Product.ID
				created = true
			}
		}
		current, e := connector.fetchProduct(ctx, configuration, credential, productID)
		if e != nil {
			return e
		}
		if !productHasSKU(current, request.SellerSKU) {
			return unsupportedOperation("product_sku_change_unsupported")
		}
		if !created && productMatches(current, request) {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.RemoteID, Duplicate: true, Reconciled: true}
			if receipt.RemoteID == "" {
				receipt.RemoteID = intString(productID)
			}
			return receipt.Validate()
		}
		if created {
			if !productMatches(current, request) {
				return ErrInvalidResponse
			}
			receipt = sdk.CommerceWriteReceipt{RemoteID: intString(productID), Applied: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]any{"product": map[string]any{"id": productID, "title": request.Title, "body_html": request.Description, "status": request.StatusRemoteID}})
		_, callErr := connector.call(ctx, configuration, credential, "PUT", "/products/"+intString(productID)+".json", nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			reconciled, reconcileErr := connector.fetchProduct(ctx, configuration, credential, productID)
			if reconcileErr == nil && productMatches(reconciled, request) {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.RemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		updated, e := connector.fetchProduct(ctx, configuration, credential, productID)
		if e != nil || !productHasSKU(updated, request.SellerSKU) || !productMatches(updated, request) {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.RemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}

func (connector *Connector) fetchVariant(ctx context.Context, configuration Configuration, credential credentials, variantID int64) (shopifyVariant, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/variants/"+intString(variantID)+".json", nil, nil)
	if err != nil {
		return shopifyVariant{}, err
	}
	var value struct {
		Variant shopifyVariant `json:"variant"`
	}
	if json.Unmarshal(response.Body, &value) != nil || value.Variant.ID != variantID {
		return shopifyVariant{}, ErrInvalidResponse
	}
	return value.Variant, nil
}

func priceMatches(variant shopifyVariant, request sdk.PriceWriteRequest) bool {
	return variant.Price == request.Value && variant.CompareAtPrice == request.CompareAt
}

func (connector *Connector) WritePrice(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PriceWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	if request.Currency != configuration.StoreCurrency {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	variantID, err := parseVariantRemoteID(request.VariantRemoteID)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		current, e := connector.fetchVariant(ctx, configuration, credential, variantID)
		if e == nil && priceMatches(current, request) {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]any{"variant": map[string]any{"id": variantID, "price": request.Value, "compare_at_price": request.CompareAt}})
		_, callErr := connector.call(ctx, configuration, credential, "PUT", "/variants/"+intString(variantID)+".json", nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			current, reconcileErr := connector.fetchVariant(ctx, configuration, credential, variantID)
			if reconcileErr == nil && priceMatches(current, request) {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		current, e = connector.fetchVariant(ctx, configuration, credential, variantID)
		if e != nil || !priceMatches(current, request) {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}

func validShopifyOrderStatus(status string) bool {
	switch status {
	case "open", "closed", "cancelled":
		return true
	default:
		return false
	}
}

func (connector *Connector) fetchOrderStatus(ctx context.Context, configuration Configuration, credential credentials, orderID int64) (string, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/orders/"+intString(orderID)+".json", []QueryParam{{Name: "fields", Value: "id,cancelled_at,closed_at"}}, nil)
	if err != nil {
		return "", err
	}
	var value struct {
		Order shopifyOrder `json:"order"`
	}
	if json.Unmarshal(response.Body, &value) != nil || value.Order.ID != orderID {
		return "", ErrInvalidResponse
	}
	return shopifyOrderStatus(value.Order), nil
}

// WriteOrderStatus only supports the three unambiguous, single-call order
// lifecycle transitions Shopify's REST API exposes directly on the order
// itself (cancel/close/reopen). Marking an order fulfilled is deliberately
// excluded: Shopify models fulfillment as its own sub-resource requiring
// line-item-level detail this SDK's single StatusRemoteID contract cannot
// carry, and getting a partial/ambiguous fulfillment wrong is worse than not
// offering it in v1.
func (connector *Connector) WriteOrderStatus(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.OrderStatusWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.status.write") != nil || request.Validate() != nil || !validShopifyOrderStatus(request.StatusRemoteID) {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	orderID, err := parsePositiveID(request.OrderRemoteID)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	path := map[string]string{"cancelled": "/orders/" + intString(orderID) + "/cancel.json", "closed": "/orders/" + intString(orderID) + "/close.json", "open": "/orders/" + intString(orderID) + "/open.json"}[request.StatusRemoteID]
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		current, e := connector.fetchOrderStatus(ctx, configuration, credential, orderID)
		if e == nil && current == request.StatusRemoteID {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		_, callErr := connector.call(ctx, configuration, credential, "POST", path, nil, []byte("{}"))
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			current, reconcileErr := connector.fetchOrderStatus(ctx, configuration, credential, orderID)
			if reconcileErr == nil && current == request.StatusRemoteID {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		current, e = connector.fetchOrderStatus(ctx, configuration, credential, orderID)
		if e != nil || current != request.StatusRemoteID {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}
