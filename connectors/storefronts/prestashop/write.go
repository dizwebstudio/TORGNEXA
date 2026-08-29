package prestashop

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func xmlEscape(v string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;").Replace(v)
}

func (c *Connector) fetchProduct(ctx context.Context, cfg Configuration, cred credentials, productID int64) (psProduct, error) {
	resp, err := c.call(ctx, cfg, cred, "GET", "/products/"+strconv.FormatInt(productID, 10), []QueryParam{{Name: "display", Value: "[id,reference,name,price,active,date_upd]"}}, nil)
	if err != nil {
		return psProduct{}, err
	}
	var env struct {
		// PrestaShop's JSON Webservice encoder keeps the collection resource
		// name even for /products/{id} (for example {"products":[...]}).
		// Older adapters/tests may return the singular envelope, so accept both
		// forms while still requiring exactly one matching resource.
		Product  psProduct   `json:"product"`
		Products []psProduct `json:"products"`
	}
	if json.Unmarshal(resp.Body, &env) != nil {
		return psProduct{}, ErrInvalidResponse
	}
	product := env.Product
	if len(env.Products) == 1 {
		product = env.Products[0]
	} else if len(env.Products) > 1 {
		return psProduct{}, ErrInvalidResponse
	}
	id, e := product.ID.Int64()
	if e != nil || id != productID {
		return psProduct{}, ErrInvalidResponse
	}
	return product, nil
}
func (c *Connector) fetchCombination(ctx context.Context, cfg Configuration, cred credentials, productID, combinationID int64) (psCombination, error) {
	resp, err := c.call(ctx, cfg, cred, "GET", "/combinations/"+strconv.FormatInt(combinationID, 10), []QueryParam{{Name: "display", Value: "[id,id_product,reference,price,date_upd]"}}, nil)
	if err != nil {
		return psCombination{}, err
	}
	var env struct {
		Combination  psCombination   `json:"combination"`
		Combinations []psCombination `json:"combinations"`
	}
	if json.Unmarshal(resp.Body, &env) != nil {
		return psCombination{}, ErrInvalidResponse
	}
	combination := env.Combination
	if len(env.Combinations) == 1 {
		combination = env.Combinations[0]
	} else if len(env.Combinations) > 1 {
		return psCombination{}, ErrInvalidResponse
	}
	id, e := combination.ID.Int64()
	pid, e2 := combination.ProductID.Int64()
	if e != nil || e2 != nil || id != combinationID || pid != productID {
		return psCombination{}, ErrInvalidResponse
	}
	return combination, nil
}
func (c *Connector) currentPrice(ctx context.Context, cfg Configuration, cred credentials, p, a int64) (string, error) {
	product, e := c.fetchProduct(ctx, cfg, cred, p)
	if e != nil {
		return "", e
	}
	if a == 0 {
		return normalizeMoney(product.Price)
	}
	comb, e := c.fetchCombination(ctx, cfg, cred, p, a)
	if e != nil {
		return "", e
	}
	return addMoney(product.Price, comb.Price)
}
func (c *Connector) WritePrice(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PriceWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.write") != nil || request.Validate() != nil || request.CompareAt != "" {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	if request.Currency != cfg.StoreCurrency {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	p, a, err := parseVariant(request.VariantRemoteID)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	target, e := normalizeMoney(request.Value)
	if e != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	var receipt sdk.CommerceWriteReceipt
	err = c.withCredentials(ctx, runtime, account.SecretReference, func(cred credentials) error {
		current, e := c.currentPrice(ctx, cfg, cred, p, a)
		if e == nil && current == target {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		var path, body string
		if a == 0 {
			path = "/products/" + strconv.FormatInt(p, 10)
			body = fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><prestashop><product><id>%d</id><price>%s</price></product></prestashop>`, p, xmlEscape(target))
		} else {
			product, e := c.fetchProduct(ctx, cfg, cred, p)
			if e != nil {
				return e
			}
			impact, e := subtractMoney(target, product.Price)
			if e != nil {
				return sdk.ErrInvalidCommerceWrite
			}
			path = "/combinations/" + strconv.FormatInt(a, 10)
			body = fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><prestashop><combination><id>%d</id><price>%s</price></combination></prestashop>`, a, xmlEscape(impact))
		}
		_, callErr := c.call(ctx, cfg, cred, "PATCH", path, nil, []byte(body))
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			current, re := c.currentPrice(ctx, cfg, cred, p, a)
			if re == nil && current == target {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		current, e = c.currentPrice(ctx, cfg, cred, p, a)
		if e != nil || current != target {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}
func (c *Connector) WriteInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.InventoryWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	p, a, err := parseVariant(request.VariantRemoteID)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	var receipt sdk.CommerceWriteReceipt
	err = c.withCredentials(ctx, runtime, account.SecretReference, func(cred credentials) error {
		stock, e := c.fetchStock(ctx, cfg, cred, p, a)
		if e != nil {
			return e
		}
		sid, e := stock.ID.Int64()
		if e != nil || sid < 1 {
			return ErrInvalidResponse
		}
		qty, e := stock.Quantity.Int64()
		if e == nil && qty == request.Quantity {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><prestashop><stock_available><id>%d</id><quantity>%d</quantity></stock_available></prestashop>`, sid, request.Quantity)
		_, callErr := c.call(ctx, cfg, cred, "PATCH", "/stock_availables/"+strconv.FormatInt(sid, 10), nil, []byte(body))
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			current, re := c.fetchStock(ctx, cfg, cred, p, a)
			if re == nil {
				q, _ := current.Quantity.Int64()
				if q == request.Quantity {
					receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true, Reconciled: true}
					return receipt.Validate()
				}
			}
			return writeOutcomeUnknown()
		}
		current, e := c.fetchStock(ctx, cfg, cred, p, a)
		if e != nil {
			return e
		}
		q, e := current.Quantity.Int64()
		if e != nil || q != request.Quantity {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}
func (c *Connector) fetchOrderState(ctx context.Context, cfg Configuration, cred credentials, orderID int64) (string, error) {
	resp, err := c.call(ctx, cfg, cred, "GET", "/orders/"+strconv.FormatInt(orderID, 10), []QueryParam{{Name: "display", Value: "[id,current_state]"}}, nil)
	if err != nil {
		return "", err
	}
	var env struct {
		Order  psOrder   `json:"order"`
		Orders []psOrder `json:"orders"`
	}
	if json.Unmarshal(resp.Body, &env) != nil {
		return "", ErrInvalidResponse
	}
	order := env.Order
	if len(env.Orders) == 1 {
		order = env.Orders[0]
	} else if len(env.Orders) > 1 {
		return "", ErrInvalidResponse
	}
	id, e := order.ID.Int64()
	if e != nil || id != orderID {
		return "", ErrInvalidResponse
	}
	state := order.CurrentState.String()
	if !validText(state, 64) {
		return "", ErrInvalidResponse
	}
	return state, nil
}
func (c *Connector) WriteOrderStatus(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.OrderStatusWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.status.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	oid, err := strconv.ParseInt(request.OrderRemoteID, 10, 64)
	state, err2 := strconv.ParseInt(request.StatusRemoteID, 10, 64)
	if err != nil || err2 != nil || oid < 1 || state < 1 {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = c.withCredentials(ctx, runtime, account.SecretReference, func(cred credentials) error {
		current, e := c.fetchOrderState(ctx, cfg, cred, oid)
		if e == nil && current == request.StatusRemoteID {
			receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><prestashop><order_history><id_order_state>%d</id_order_state><id_order>%d</id_order></order_history></prestashop>`, state, oid)
		_, callErr := c.call(ctx, cfg, cred, "POST", "/order_histories", nil, []byte(body))
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			current, re := c.fetchOrderState(ctx, cfg, cred, oid)
			if re == nil && current == request.StatusRemoteID {
				receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		current, e = c.fetchOrderState(ctx, cfg, cred, oid)
		if e != nil || current != request.StatusRemoteID {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.OrderRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}
