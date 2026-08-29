package opencart

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func productMatches(p bridgeProduct, req sdk.ProductWriteRequest) bool {
	return p.SKU == req.SellerSKU && p.Title == req.Title && normalizeProductStatus(p.Status) == normalizeProductStatus(req.StatusRemoteID)
}

func normalizeProductStatus(status string) string {
	normalized := strings.ToLower(strings.TrimSpace(status))

	switch normalized {
	case "private", "archived":
		// OpenCart only has published/unpublished products. The bridge maps
		// provider-neutral private/archived states to its unpublished state.
		return "draft"
	default:
		return normalized
	}
}
func (c *Connector) findProductBySKU(ctx context.Context, cfg Configuration, cred credentials, sku string) (bridgeProduct, bool, error) {
	resp, e := c.call(ctx, cfg, cred, "GET", "product-by-sku", []QueryParam{{Name: "sku", Value: sku}}, nil)
	if e != nil {
		var remote *sdk.RemoteError
		if errors.As(e, &remote) && remote.Category == sdk.ErrorNotFound {
			return bridgeProduct{}, false, nil
		}
		return bridgeProduct{}, false, e
	}
	var p bridgeProduct
	if json.Unmarshal(resp.Body, &p) != nil || p.ID < 1 {
		return bridgeProduct{}, false, ErrInvalidResponse
	}
	return p, true, nil
}
func (c *Connector) fetchProduct(ctx context.Context, cfg Configuration, cred credentials, id int64) (bridgeProduct, error) {
	resp, e := c.call(ctx, cfg, cred, "GET", "product", []QueryParam{{Name: "id", Value: strconv.FormatInt(id, 10)}}, nil)
	if e != nil {
		return bridgeProduct{}, e
	}
	var p bridgeProduct
	if json.Unmarshal(resp.Body, &p) != nil || p.ID != id {
		return bridgeProduct{}, ErrInvalidResponse
	}
	return p, nil
}
func (c *Connector) UpsertProduct(ctx context.Context, a sdk.Account, r sdk.Runtime, req sdk.ProductWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.write") != nil || req.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.CommerceWriteReceipt{}, e
	}
	var receipt sdk.CommerceWriteReceipt
	e = c.withCredentials(ctx, r, a.SecretReference, func(cred credentials) error {
		var existing bridgeProduct
		found := false
		if req.RemoteID != "" {
			id, pe := strconv.ParseInt(req.RemoteID, 10, 64)
			if pe != nil || id < 1 {
				return sdk.ErrInvalidCommerceWrite
			}
			existing, pe = c.fetchProduct(ctx, cfg, cred, id)
			if pe != nil {
				return pe
			}
			found = true
		} else {
			var pe error
			existing, found, pe = c.findProductBySKU(ctx, cfg, cred, req.SellerSKU)
			if pe != nil {
				return pe
			}
		}
		if found && productMatches(existing, req) {
			receipt = sdk.CommerceWriteReceipt{RemoteID: strconv.FormatInt(existing.ID, 10), Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]any{"id": existing.ID, "sku": req.SellerSKU, "title": req.Title, "description": req.Description, "status": req.StatusRemoteID, "idempotency_key": req.IdempotencyKey})
		method, route := "POST", "product"
		if found {
			method = "PUT"
		}
		resp, callErr := c.call(ctx, cfg, cred, method, route, nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			p, ok, re := c.findProductBySKU(ctx, cfg, cred, req.SellerSKU)
			if re == nil && ok && productMatches(p, req) {
				receipt = sdk.CommerceWriteReceipt{RemoteID: strconv.FormatInt(p.ID, 10), Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		var written bridgeProduct
		if json.Unmarshal(resp.Body, &written) != nil || written.ID < 1 || !productMatches(written, req) {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: strconv.FormatInt(written.ID, 10), Applied: true}
		return receipt.Validate()
	})
	return receipt, e
}
func (c *Connector) WritePrice(ctx context.Context, a sdk.Account, r sdk.Runtime, req sdk.PriceWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.write") != nil || req.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.CommerceWriteReceipt{}, e
	}
	if req.Currency != cfg.StoreCurrency {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	var receipt sdk.CommerceWriteReceipt
	e = c.withCredentials(ctx, r, a.SecretReference, func(cred credentials) error {
		v, e := c.fetchVariant(ctx, cfg, cred, req.VariantRemoteID)
		if e == nil && v.Price == req.Value && v.CompareAt == req.CompareAt {
			receipt = sdk.CommerceWriteReceipt{RemoteID: req.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]any{"remote_id": req.VariantRemoteID, "price": req.Value, "compare_at": req.CompareAt, "currency": req.Currency, "idempotency_key": req.IdempotencyKey})
		_, callErr := c.call(ctx, cfg, cred, "PUT", "variant-price", nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			v, re := c.fetchVariant(ctx, cfg, cred, req.VariantRemoteID)
			if re == nil && v.Price == req.Value && v.CompareAt == req.CompareAt {
				receipt = sdk.CommerceWriteReceipt{RemoteID: req.VariantRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		v, e = c.fetchVariant(ctx, cfg, cred, req.VariantRemoteID)
		if e != nil || v.Price != req.Value || v.CompareAt != req.CompareAt {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: req.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, e
}
func (c *Connector) WriteInventory(ctx context.Context, a sdk.Account, r sdk.Runtime, req sdk.InventoryWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.write") != nil || req.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	if req.LocationRemoteID != "" && req.LocationRemoteID != storeLocationID {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.CommerceWriteReceipt{}, e
	}
	var receipt sdk.CommerceWriteReceipt
	e = c.withCredentials(ctx, r, a.SecretReference, func(cred credentials) error {
		v, e := c.fetchVariant(ctx, cfg, cred, req.VariantRemoteID)
		if e == nil && v.Quantity == req.Quantity {
			receipt = sdk.CommerceWriteReceipt{RemoteID: req.VariantRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]any{"remote_id": req.VariantRemoteID, "quantity": req.Quantity, "idempotency_key": req.IdempotencyKey})
		_, callErr := c.call(ctx, cfg, cred, "PUT", "variant-inventory", nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			v, re := c.fetchVariant(ctx, cfg, cred, req.VariantRemoteID)
			if re == nil && v.Quantity == req.Quantity {
				receipt = sdk.CommerceWriteReceipt{RemoteID: req.VariantRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		v, e = c.fetchVariant(ctx, cfg, cred, req.VariantRemoteID)
		if e != nil || v.Quantity != req.Quantity {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: req.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, e
}
func (c *Connector) fetchOrderStatus(ctx context.Context, cfg Configuration, cred credentials, id int64) (string, error) {
	resp, e := c.call(ctx, cfg, cred, "GET", "order", []QueryParam{{Name: "id", Value: strconv.FormatInt(id, 10)}}, nil)
	if e != nil {
		return "", e
	}
	var o bridgeOrder
	if json.Unmarshal(resp.Body, &o) != nil || o.ID != id || !validText(o.StatusRemoteID, 64) {
		return "", ErrInvalidResponse
	}
	return o.StatusRemoteID, nil
}
func (c *Connector) WriteOrderStatus(ctx context.Context, a sdk.Account, r sdk.Runtime, req sdk.OrderStatusWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.status.write") != nil || req.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	id, e := strconv.ParseInt(req.OrderRemoteID, 10, 64)
	if e != nil || id < 1 {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.CommerceWriteReceipt{}, e
	}
	var receipt sdk.CommerceWriteReceipt
	e = c.withCredentials(ctx, r, a.SecretReference, func(cred credentials) error {
		status, e := c.fetchOrderStatus(ctx, cfg, cred, id)
		if e == nil && status == req.StatusRemoteID {
			receipt = sdk.CommerceWriteReceipt{RemoteID: req.OrderRemoteID, Duplicate: true, Reconciled: true}
			return receipt.Validate()
		}
		body, _ := json.Marshal(map[string]any{"id": id, "status_remote_id": req.StatusRemoteID, "idempotency_key": req.IdempotencyKey})
		_, callErr := c.call(ctx, cfg, cred, "PUT", "order-status", nil, body)
		if callErr != nil {
			if !isAmbiguousWrite(callErr) {
				return callErr
			}
			status, re := c.fetchOrderStatus(ctx, cfg, cred, id)
			if re == nil && status == req.StatusRemoteID {
				receipt = sdk.CommerceWriteReceipt{RemoteID: req.OrderRemoteID, Applied: true, Reconciled: true}
				return receipt.Validate()
			}
			return writeOutcomeUnknown()
		}
		status, e = c.fetchOrderStatus(ctx, cfg, cred, id)
		if e != nil || status != req.StatusRemoteID {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: req.OrderRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, e
}
