package shopify

import (
	"context"
	"encoding/json"
	"errors"

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
	return product.ID > 0 && len(product.Variants) > 0 && product.Title == request.Title && product.Status == request.StatusRemoteID
}

// UpsertProduct only supports updating an already-admitted product's
// title/description/status. Two deliberate exclusions, not oversights:
//   - Create (RemoteID == "") is unsupported: Shopify's REST API has no
//     reliable cross-catalog SKU search (only per-product listing or the
//     GraphQL Admin API), so the find-or-create-by-SKU idempotency pattern
//     WooCommerce/OpenCart use cannot be implemented safely here without
//     risking duplicate products on a retried create.
//   - Changing SellerSKU on an existing product is unsupported: the SKU
//     lives on the variant sub-resource, a second API call from the product
//     resource, and this repository's ambiguous-write reconciliation model
//     assumes one resource per write — bolting on a second call here would
//     weaken that guarantee rather than extend it.
func (connector *Connector) UpsertProduct(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ProductWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.write") != nil || request.Validate() != nil || !validShopifyProductStatus(request.StatusRemoteID) {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	if request.RemoteID == "" {
		return sdk.CommerceWriteReceipt{}, unsupportedOperation("product_create_unsupported")
	}
	productID, err := parsePositiveID(request.RemoteID)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		current, e := connector.fetchProduct(ctx, configuration, credential, productID)
		if e != nil {
			return e
		}
		if current.Variants[0].SKU != request.SellerSKU {
			return unsupportedOperation("product_sku_change_unsupported")
		}
		if productMatches(current, request) {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.RemoteID, Duplicate: true, Reconciled: true}
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
		if e != nil || !productMatches(updated, request) {
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
