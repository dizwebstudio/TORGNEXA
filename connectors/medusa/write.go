package medusa

import (
	"context"
	"encoding/json"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func (connector *Connector) fetchProduct(ctx context.Context, configuration Configuration, credential credentials, productID string) (medusaProduct, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/products/"+productID, []QueryParam{{Name: "fields", Value: "id,title,status,description,variants.id,variants.sku"}}, nil)
	if err != nil {
		return medusaProduct{}, err
	}
	var value struct {
		Product medusaProduct `json:"product"`
	}
	if json.Unmarshal(response.Body, &value) != nil || value.Product.ID != productID || len(value.Product.Variants) == 0 {
		return medusaProduct{}, ErrInvalidResponse
	}
	return value.Product, nil
}

func productMatches(product medusaProduct, request sdk.ProductWriteRequest) bool {
	return product.ID != "" && len(product.Variants) > 0 && product.Title == request.Title && product.Status == request.StatusRemoteID && product.Description == request.Description
}

// UpsertProduct only supports updating an already-admitted product's
// title/description/status, the same deliberate scope as the Shopify
// connector and for the same reason: create (RemoteID == "") is unsupported
// because there is no reliable exact-match, catalog-wide SKU lookup to make
// find-or-create idempotent, and changing SellerSKU on an existing product
// is unsupported because the SKU lives on the variant sub-resource, a
// second API call this repository's ambiguous-write reconciliation model
// (one resource per write) does not extend to.
func (connector *Connector) UpsertProduct(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ProductWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.write") != nil || request.Validate() != nil {
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
		current, e := connector.fetchProduct(ctx, configuration, credential, request.RemoteID)
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
		body, _ := json.Marshal(map[string]any{"title": request.Title, "description": request.Description, "status": request.StatusRemoteID})
		_, callErr := connector.call(ctx, configuration, credential, "POST", "/products/"+request.RemoteID, nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			reconciled, reconcileErr := connector.fetchProduct(ctx, configuration, credential, request.RemoteID)
			if reconcileErr == nil && productMatches(reconciled, request) {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.RemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		updated, e := connector.fetchProduct(ctx, configuration, credential, request.RemoteID)
		if e != nil || !productMatches(updated, request) {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.RemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}

func (connector *Connector) fetchVariantPrice(ctx context.Context, configuration Configuration, credential credentials, productID, variantID, currency string) (string, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/products/"+productID+"/variants/"+variantID, []QueryParam{{Name: "fields", Value: "id,prices.amount,prices.currency_code"}}, nil)
	if err != nil {
		return "", err
	}
	var value struct {
		Variant medusaVariant `json:"variant"`
	}
	if json.Unmarshal(response.Body, &value) != nil || value.Variant.ID != variantID {
		return "", ErrInvalidResponse
	}
	for _, price := range value.Variant.Prices {
		if strings.EqualFold(price.CurrencyCode, currency) {
			return price.Amount.String(), nil
		}
	}
	return "", nil
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
	productID, variantID, err := parseVariantRemoteID(request.VariantRemoteID)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	currency := strings.ToLower(request.Currency)
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		current, e := connector.fetchVariantPrice(ctx, configuration, credential, productID, variantID, currency)
		if e == nil && current == request.Value {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]any{"prices": []map[string]any{{"currency_code": currency, "amount": json.Number(request.Value)}}})
		_, callErr := connector.call(ctx, configuration, credential, "POST", "/products/"+productID+"/variants/"+variantID, nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			current, reconcileErr := connector.fetchVariantPrice(ctx, configuration, credential, productID, variantID, currency)
			if reconcileErr == nil && current == request.Value {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		current, e = connector.fetchVariantPrice(ctx, configuration, credential, productID, variantID, currency)
		if e != nil || current != request.Value {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}

func (connector *Connector) fetchOrderStatus(ctx context.Context, configuration Configuration, credential credentials, orderID string) (string, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/orders/"+orderID, []QueryParam{{Name: "fields", Value: "id,status"}}, nil)
	if err != nil {
		return "", err
	}
	var value struct {
		Order medusaOrder `json:"order"`
	}
	if json.Unmarshal(response.Body, &value) != nil || value.Order.ID != orderID || !validRemoteText(value.Order.Status, 64) {
		return "", ErrInvalidResponse
	}
	return value.Order.Status, nil
}

// WriteOrderStatus only supports canceling an order, the single unambiguous
// single-call lifecycle transition Medusa's REST API exposes directly
// (POST /admin/orders/{id}/cancel). Every other order.status transition in
// Medusa is a side effect of a workflow (fulfillment, payment capture, ...)
// with its own sub-resource and request shape, not a settable field.
func (connector *Connector) WriteOrderStatus(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.OrderStatusWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.status.write") != nil || request.Validate() != nil || request.StatusRemoteID != "canceled" {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		current, e := connector.fetchOrderStatus(ctx, configuration, credential, request.OrderRemoteID)
		if e == nil && current == "canceled" {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		_, callErr := connector.call(ctx, configuration, credential, "POST", "/orders/"+request.OrderRemoteID+"/cancel", nil, []byte("{}"))
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			current, reconcileErr := connector.fetchOrderStatus(ctx, configuration, credential, request.OrderRemoteID)
			if reconcileErr == nil && current == "canceled" {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		current, e = connector.fetchOrderStatus(ctx, configuration, credential, request.OrderRemoteID)
		if e != nil || current != "canceled" {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}
