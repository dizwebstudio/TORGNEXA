package woocommerce

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

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

func (connector *Connector) findProductBySKU(ctx context.Context, configuration Configuration, credential credentials, sku string) (wooProduct, bool, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/products", []QueryParam{{Name: "sku", Value: sku}, {Name: "per_page", Value: "2"}}, nil)
	if err != nil {
		return wooProduct{}, false, err
	}
	var rows []wooProduct
	if json.Unmarshal(response.Body, &rows) != nil || len(rows) > 1 {
		return wooProduct{}, false, ErrInvalidResponse
	}
	if len(rows) == 0 {
		return wooProduct{}, false, nil
	}
	if rows[0].ID < 1 || rows[0].SKU != sku {
		return wooProduct{}, false, ErrInvalidResponse
	}
	return rows[0], true, nil
}

func productMatches(product wooProduct, request sdk.ProductWriteRequest) bool {
	return product.ID > 0 && product.SKU == request.SellerSKU && product.Name == request.Title && product.Description == request.Description && product.Status == request.StatusRemoteID
}

func productBody(request sdk.ProductWriteRequest, create bool) []byte {
	value := map[string]any{"name": request.Title, "sku": request.SellerSKU, "description": request.Description, "status": request.StatusRemoteID}
	if create {
		value["type"] = "simple"
	}
	body, _ := json.Marshal(value)
	return body
}

func (connector *Connector) UpsertProduct(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ProductWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		if request.RemoteID == "" {
			body := productBody(request, true)
			if existing, found, e := connector.findProductBySKU(ctx, configuration, credential, request.SellerSKU); e != nil {
				return e
			} else if found {
				if !productMatches(existing, request) {
					remote, _ := sdk.NewRemoteError(sdk.ErrorConflict, "sku_conflict", "", 0)
					return remote
				}
				receipt = sdk.CommerceWriteReceipt{RemoteID: intString(existing.ID), Duplicate: true, Reconciled: true}
				return receipt.Validate()
			}
			response, callErr := connector.call(ctx, configuration, credential, "POST", "/products", nil, body)
			if callErr != nil {
				if !isAmbiguousWrite(callErr) {
					return callErr
				}
				existing, found, reconcileErr := connector.findProductBySKU(ctx, configuration, credential, request.SellerSKU)
				if reconcileErr == nil && found && productMatches(existing, request) {
					receipt = sdk.CommerceWriteReceipt{RemoteID: intString(existing.ID), Applied: true, Reconciled: true}
					return receipt.Validate()
				}
				return writeOutcomeUnknown()
			}
			var created wooProduct
			if json.Unmarshal(response.Body, &created) != nil || !productMatches(created, request) {
				return ErrInvalidResponse
			}
			receipt = sdk.CommerceWriteReceipt{RemoteID: intString(created.ID), Applied: true}
			return receipt.Validate()
		}
		productID, e := strconv.ParseInt(request.RemoteID, 10, 64)
		if e != nil || productID < 1 {
			return sdk.ErrInvalidCommerceWrite
		}
		body := productBody(request, false)
		response, callErr := connector.call(ctx, configuration, credential, "PUT", "/products/"+intString(productID), nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			current, found, reconcileErr := connector.findProductBySKU(ctx, configuration, credential, request.SellerSKU)
			if reconcileErr == nil && found && current.ID == productID && productMatches(current, request) {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.RemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		var updated wooProduct
		if json.Unmarshal(response.Body, &updated) != nil || updated.ID != productID || !productMatches(updated, request) {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.RemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}

func priceBody(request sdk.PriceWriteRequest) []byte {
	regular, sale := request.Value, ""
	if request.CompareAt != "" && request.CompareAt != request.Value {
		regular, sale = request.CompareAt, request.Value
	}
	body, _ := json.Marshal(map[string]any{"regular_price": regular, "sale_price": sale})
	return body
}

func (connector *Connector) fetchVariantState(ctx context.Context, configuration Configuration, credential credentials, target variantTarget) (wooVariation, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", target.path(), nil, nil)
	if err != nil {
		return wooVariation{}, err
	}
	if target.VariationID > 0 {
		var value wooVariation
		if json.Unmarshal(response.Body, &value) != nil || value.ID != target.VariationID {
			return wooVariation{}, ErrInvalidResponse
		}
		return value, nil
	}
	var product wooProduct
	if json.Unmarshal(response.Body, &product) != nil || product.ID != target.ProductID {
		return wooVariation{}, ErrInvalidResponse
	}
	return wooVariation{ID: product.ID, Price: product.Price, RegularPrice: product.RegularPrice, SalePrice: product.SalePrice, StockQuantity: product.StockQuantity, StockStatus: product.StockStatus, DateModifiedGMT: product.DateModifiedGMT}, nil
}

func priceMatches(value wooVariation, request sdk.PriceWriteRequest) bool {
	if request.CompareAt != "" && request.CompareAt != request.Value {
		return value.RegularPrice == request.CompareAt && value.SalePrice == request.Value
	}
	return value.RegularPrice == request.Value && value.SalePrice == ""
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
	target, err := parseVariantTarget(request.VariantRemoteID)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		current, e := connector.fetchVariantState(ctx, configuration, credential, target)
		if e == nil && priceMatches(current, request) {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		_, callErr := connector.call(ctx, configuration, credential, "PUT", target.path(), nil, priceBody(request))
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			current, reconcileErr := connector.fetchVariantState(ctx, configuration, credential, target)
			if reconcileErr == nil && priceMatches(current, request) {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		current, e = connector.fetchVariantState(ctx, configuration, credential, target)
		if e != nil || !priceMatches(current, request) {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}

func (connector *Connector) WriteInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.InventoryWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	if request.LocationRemoteID != "" && request.LocationRemoteID != storeLocationID {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	target, err := parseVariantTarget(request.VariantRemoteID)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		current, e := connector.fetchStock(ctx, configuration, credential, target)
		if e == nil && current == request.Quantity {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]any{"manage_stock": true, "stock_quantity": request.Quantity})
		_, callErr := connector.call(ctx, configuration, credential, "PUT", target.path(), nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			current, reconcileErr := connector.fetchStock(ctx, configuration, credential, target)
			if reconcileErr == nil && current == request.Quantity {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		current, e = connector.fetchStock(ctx, configuration, credential, target)
		if e != nil || current != request.Quantity {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}

func validWooOrderStatus(status string) bool {
	switch status {
	case "pending", "processing", "on-hold", "completed", "cancelled", "refunded", "failed":
		return true
	default:
		return false
	}
}

func (connector *Connector) fetchOrderStatus(ctx context.Context, configuration Configuration, credential credentials, orderID int64) (string, error) {
	response, err := connector.call(ctx, configuration, credential, "GET", "/orders/"+intString(orderID), nil, nil)
	if err != nil {
		return "", err
	}
	var value struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	if json.Unmarshal(response.Body, &value) != nil || value.ID != orderID || !validWooOrderStatus(value.Status) {
		return "", ErrInvalidResponse
	}
	return value.Status, nil
}

func (connector *Connector) WriteOrderStatus(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.OrderStatusWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.status.write") != nil || request.Validate() != nil || !validWooOrderStatus(request.StatusRemoteID) {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	orderID, err := strconv.ParseInt(request.OrderRemoteID, 10, 64)
	if err != nil || orderID < 1 {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		current, e := connector.fetchOrderStatus(ctx, configuration, credential, orderID)
		if e == nil && current == request.StatusRemoteID {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]string{"status": request.StatusRemoteID})
		_, callErr := connector.call(ctx, configuration, credential, "PUT", "/orders/"+intString(orderID), nil, body)
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
