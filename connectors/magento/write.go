package magento

import (
	"context"
	"encoding/json"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func validMagentoProductStatus(status string) bool {
	return status == "enabled" || status == "disabled"
}
func magentoProductStatusCode(status string) int {
	if status == "enabled" {
		return 1
	}
	return 2
}

func (connector *Connector) fetchProduct(ctx context.Context, configuration Configuration, credential credentials, sku string) (magentoProduct, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/products/"+pathSKU(sku), nil, nil)
	if err != nil {
		return magentoProduct{}, err
	}
	var product magentoProduct
	if json.Unmarshal(response.Body, &product) != nil || product.SKU != sku {
		return magentoProduct{}, ErrInvalidResponse
	}
	return product, nil
}

func productMatches(product magentoProduct, request sdk.ProductWriteRequest) bool {
	active := magentoProductStatusCode(request.StatusRemoteID)
	return product.SKU == request.SellerSKU && product.Name == request.Title && product.Status == active && product.description() == request.Description
}

// UpsertProduct only supports updating an already-admitted product.
// Creating a new Magento product (RemoteID == "") is deliberately
// unsupported: unlike Shopify/Medusa this is not a SKU-search reliability
// problem (Magento addresses products by SKU directly, so existence is a
// simple, reliable GET-by-SKU), it is that Magento requires a price,
// type_id and attribute_set_id to create a valid product, none of which
// sdk.ProductWriteRequest carries — defaulting them (a placeholder price
// or an assumed default attribute set id, which is not guaranteed stable
// across installs) would put a real, publicly visible product live wrong.
func (connector *Connector) UpsertProduct(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ProductWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.write") != nil || request.Validate() != nil || !validMagentoProductStatus(request.StatusRemoteID) {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	if request.RemoteID == "" {
		return sdk.CommerceWriteReceipt{}, unsupportedOperation("product_create_unsupported")
	}
	// RemoteID is the SKU itself for Magento (see ReadProducts), so a
	// mismatch here would mean a SKU-rename request. Magento does support
	// renaming a SKU via this same PUT, but this connector's write/read/
	// reconcile-after-ambiguous-failure verification always re-fetches by
	// RemoteID, which would no longer resolve once renamed; rejecting is
	// safer than a verification path that silently stops working.
	if request.RemoteID != request.SellerSKU {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
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
		if productMatches(current, request) {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.RemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]any{"product": map[string]any{
			"sku": request.SellerSKU, "name": request.Title, "status": magentoProductStatusCode(request.StatusRemoteID),
			"custom_attributes": []map[string]any{{"attribute_code": "description", "value": request.Description}},
		}})
		_, callErr := connector.call(ctx, configuration, credential, "PUT", "/products/"+pathSKU(request.RemoteID), nil, body)
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
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		current, e := connector.fetchProduct(ctx, configuration, credential, request.VariantRemoteID)
		if e == nil && current.Price.String() == request.Value {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]any{"product": map[string]any{"sku": request.VariantRemoteID, "price": json.Number(request.Value)}})
		_, callErr := connector.call(ctx, configuration, credential, "PUT", "/products/"+pathSKU(request.VariantRemoteID), nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			current, reconcileErr := connector.fetchProduct(ctx, configuration, credential, request.VariantRemoteID)
			if reconcileErr == nil && current.Price.String() == request.Value {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		current, e = connector.fetchProduct(ctx, configuration, credential, request.VariantRemoteID)
		if e != nil || current.Price.String() != request.Value {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}

func (connector *Connector) fetchOrderStatus(ctx context.Context, configuration Configuration, credential credentials, orderID string) (string, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/orders/"+orderID, nil, nil)
	if err != nil {
		return "", err
	}
	var order magentoOrder
	if json.Unmarshal(response.Body, &order) != nil || order.EntityID.String() != orderID || !validRemoteText(order.Status, 64) {
		return "", ErrInvalidResponse
	}
	return order.Status, nil
}

// WriteOrderStatus only supports canceling an order via Magento's own
// POST /V1/orders/{id}/cancel action. Every other Magento order status is
// a side effect of invoicing/shipment/hold workflows with their own request
// shapes, not a directly settable field.
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
