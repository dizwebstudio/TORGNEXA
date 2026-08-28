package shopware

import (
	"context"
	"encoding/json"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func validShopwareProductStatus(status string) bool {
	return status == "active" || status == "inactive"
}

type shopwareProductDetail struct {
	ID            string `json:"id"`
	ProductNumber string `json:"productNumber"`
	Name          string `json:"name"`
	Active        bool   `json:"active"`
	Description   string `json:"description"`
}

func (connector *Connector) fetchProductDetail(ctx context.Context, configuration Configuration, accountID string, credential credentials, productID string) (shopwareProductDetail, error) {
	response, err := connector.call(ctx, configuration, accountID, credential, "GET", "/product/"+productID, nil, nil)
	if err != nil {
		return shopwareProductDetail{}, err
	}
	var value struct {
		Data shopwareProductDetail `json:"data"`
	}
	if json.Unmarshal(response.Body, &value) != nil || value.Data.ID != productID {
		return shopwareProductDetail{}, ErrInvalidResponse
	}
	return value.Data, nil
}

func productMatches(product shopwareProductDetail, request sdk.ProductWriteRequest) bool {
	active := request.StatusRemoteID == "active"
	return product.ID != "" && product.ProductNumber == request.SellerSKU && product.Name == request.Title && product.Active == active && product.Description == request.Description
}

// UpsertProduct only supports updating an already-admitted product.
// Creating a new Shopware product (RemoteID == "") is deliberately
// unsupported: unlike Shopify/Medusa this is not a SKU-search reliability
// problem (Shopware's Criteria filter on productNumber IS an exact,
// reliable match), it is that Shopware requires a taxId and at least one
// price entry to create a valid product, neither of which
// sdk.ProductWriteRequest carries — defaulting them (e.g. a placeholder
// zero price) would put a real, publicly visible product live at the wrong
// price, which is worse than refusing outright.
func (connector *Connector) UpsertProduct(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ProductWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.write") != nil || request.Validate() != nil || !validShopwareProductStatus(request.StatusRemoteID) {
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
		current, e := connector.fetchProductDetail(ctx, configuration, account.ID, credential, request.RemoteID)
		if e != nil {
			return e
		}
		if productMatches(current, request) {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.RemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]any{"productNumber": request.SellerSKU, "name": request.Title, "description": request.Description, "active": request.StatusRemoteID == "active"})
		_, callErr := connector.call(ctx, configuration, account.ID, credential, "PATCH", "/product/"+request.RemoteID, nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			reconciled, reconcileErr := connector.fetchProductDetail(ctx, configuration, account.ID, credential, request.RemoteID)
			if reconcileErr == nil && productMatches(reconciled, request) {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.RemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		updated, e := connector.fetchProductDetail(ctx, configuration, account.ID, credential, request.RemoteID)
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
		currencyID, currencyErr := connector.currencyID(ctx, configuration, account.ID, credential)
		if currencyErr != nil {
			return currencyErr
		}
		fetchPrice := func() (string, bool, error) {
			response, e := connector.call(ctx, configuration, account.ID, credential, "GET", "/product/"+request.VariantRemoteID, nil, nil)
			if e != nil {
				return "", false, e
			}
			var value struct {
				Data struct {
					ID    string          `json:"id"`
					Price []shopwarePrice `json:"price"`
				} `json:"data"`
			}
			if json.Unmarshal(response.Body, &value) != nil || value.Data.ID != request.VariantRemoteID {
				return "", false, ErrInvalidResponse
			}
			current, found := priceForCurrency(value.Data.Price, currencyID)
			return current, found, nil
		}
		current, found, e := fetchPrice()
		if e == nil && found && current == request.Value {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]any{"price": []map[string]any{{"currencyId": currencyID, "gross": json.Number(request.Value), "net": json.Number(request.Value), "linked": false}}})
		_, callErr := connector.call(ctx, configuration, account.ID, credential, "PATCH", "/product/"+request.VariantRemoteID, nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			current, found, reconcileErr := fetchPrice()
			if reconcileErr == nil && found && current == request.Value {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		current, found, e = fetchPrice()
		if e != nil || !found || current != request.Value {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}

func (connector *Connector) fetchOrderStatus(ctx context.Context, configuration Configuration, accountID string, credential credentials, orderID string) (string, error) {
	body, _ := json.Marshal(map[string]any{"associations": map[string]any{"stateMachineState": map[string]any{}}, "filter": []map[string]any{{"type": "equals", "field": "id", "value": orderID}}, "limit": 1})
	response, err := connector.call(ctx, configuration, accountID, credential, "POST", "/search/order", nil, body)
	if err != nil {
		return "", err
	}
	var result struct {
		Data []shopwareOrder `json:"data"`
	}
	if json.Unmarshal(response.Body, &result) != nil || len(result.Data) != 1 || result.Data[0].StateMachineState == nil || !validRemoteText(result.Data[0].StateMachineState.TechnicalName, 64) {
		return "", ErrInvalidResponse
	}
	return result.Data[0].StateMachineState.TechnicalName, nil
}

// WriteOrderStatus only supports canceling an order via Shopware's own
// order state machine transition action
// (POST /api/_action/order/{id}/state/cancel). Every other transition
// (process, complete, reopen, ...) depends on the order's current state
// machine position in ways a single generic StatusRemoteID write cannot
// safely encode.
func (connector *Connector) WriteOrderStatus(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.OrderStatusWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.status.write") != nil || request.Validate() != nil || request.StatusRemoteID != "cancelled" {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		current, e := connector.fetchOrderStatus(ctx, configuration, account.ID, credential, request.OrderRemoteID)
		if e == nil && current == "cancelled" {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		_, callErr := connector.call(ctx, configuration, account.ID, credential, "POST", "/_action/order/"+request.OrderRemoteID+"/state/cancel", nil, []byte("{}"))
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			current, reconcileErr := connector.fetchOrderStatus(ctx, configuration, account.ID, credential, request.OrderRemoteID)
			if reconcileErr == nil && current == "cancelled" {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		current, e = connector.fetchOrderStatus(ctx, configuration, account.ID, credential, request.OrderRemoteID)
		if e != nil || current != "cancelled" {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}
