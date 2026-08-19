package prestashop

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func (c *Connector) listProducts(ctx context.Context, cfg Configuration, cred credentials, offset, limit int) ([]psProduct, error) {
	resp, err := c.call(ctx, cfg, cred, "GET", "/products", []QueryParam{{Name: "display", Value: "[id,reference,name,price,active,date_upd]"}, {Name: "sort", Value: "[id_ASC]"}, {Name: "limit", Value: strconv.Itoa(offset) + "," + strconv.Itoa(limit)}}, nil)
	if err != nil {
		return nil, err
	}
	var env struct {
		Products []psProduct `json:"products"`
	}
	if json.Unmarshal(resp.Body, &env) != nil || len(env.Products) > limit {
		return nil, ErrInvalidResponse
	}
	return env.Products, nil
}
func (c *Connector) listCombinations(ctx context.Context, cfg Configuration, cred credentials, productID int64) ([]psCombination, error) {
	resp, err := c.call(ctx, cfg, cred, "GET", "/combinations", []QueryParam{{Name: "display", Value: "[id,id_product,reference,price,date_upd]"}, {Name: "filter[id_product]", Value: "[" + strconv.FormatInt(productID, 10) + "]"}, {Name: "sort", Value: "[id_ASC]"}, {Name: "limit", Value: "1000"}}, nil)
	if err != nil {
		return nil, err
	}
	var env struct {
		Combinations []psCombination `json:"combinations"`
	}
	if json.Unmarshal(resp.Body, &env) != nil || len(env.Combinations) > 1000 {
		return nil, ErrInvalidResponse
	}
	return env.Combinations, nil
}
func (c *Connector) ReadProducts(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ProductPage, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || request.Validate(50) != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.ProductPage{}, err
	}
	offset, err := decodePageCursor(request.Cursor, cfg.fingerprint("products"))
	if err != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	var out sdk.ProductPage
	err = c.withCredentials(ctx, runtime, account.SecretReference, func(cred credentials) error {
		products, e := c.listProducts(ctx, cfg, cred, offset, request.Limit)
		if e != nil {
			return e
		}
		items := make([]sdk.RemoteProduct, 0, len(products))
		for _, p := range products {
			pid, e := p.ID.Int64()
			if e != nil || pid < 1 {
				return ErrInvalidResponse
			}
			title, e := localizedString(p.Name)
			if e != nil || !validText(title, 500) {
				return ErrInvalidResponse
			}
			updated, e := parsePSTime(p.DateUpd)
			if e != nil {
				return e
			}
			combinations, e := c.listCombinations(ctx, cfg, cred, pid)
			if e != nil {
				return e
			}
			variants := make([]sdk.RemoteVariant, 0, max(1, len(combinations)))
			sku := p.Reference
			if len(combinations) == 0 {
				if !validText(sku, 200) {
					return ErrInvalidResponse
				}
				variants = append(variants, sdk.RemoteVariant{RemoteID: variantRemoteID(pid, 0), SKUs: []string{sku}})
			} else {
				for _, comb := range combinations {
					cid, e := comb.ID.Int64()
					if e != nil || cid < 1 || !validText(comb.Reference, 200) {
						return ErrInvalidResponse
					}
					if sku == "" {
						sku = comb.Reference
					}
					variants = append(variants, sdk.RemoteVariant{RemoteID: variantRemoteID(pid, cid), SKUs: []string{comb.Reference}})
				}
				if !validText(sku, 200) {
					return ErrInvalidResponse
				}
			}
			item := sdk.RemoteProduct{RemoteID: strconv.FormatInt(pid, 10), SellerSKU: sku, Title: title, UpdatedAt: updated, Variants: variants}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, e := nextCursor(offset, request.Limit, len(products), cfg.fingerprint("products"))
		if e != nil {
			return e
		}
		out = sdk.ProductPage{Items: items, NextCursor: next}
		return out.Validate(request.Limit)
	})
	return out, err
}

func (c *Connector) ReadPrices(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.PricePage, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.read") != nil || request.Validate(50) != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.PricePage{}, err
	}
	offset, err := decodePageCursor(request.Cursor, cfg.fingerprint("prices"))
	if err != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	var out sdk.PricePage
	err = c.withCredentials(ctx, runtime, account.SecretReference, func(cred credentials) error {
		products, e := c.listProducts(ctx, cfg, cred, offset, request.Limit)
		if e != nil {
			return e
		}
		items := []sdk.RemotePrice{}
		for _, p := range products {
			pid, e := p.ID.Int64()
			if e != nil {
				return e
			}
			updated, e := parsePSTime(p.DateUpd)
			if e != nil {
				return e
			}
			combinations, e := c.listCombinations(ctx, cfg, cred, pid)
			if e != nil {
				return e
			}
			if len(combinations) == 0 {
				price, e := normalizeMoney(p.Price)
				if e != nil {
					return e
				}
				item := sdk.RemotePrice{VariantRemoteID: variantRemoteID(pid, 0), Value: price, Currency: cfg.StoreCurrency, UpdatedAt: updated}
				if item.Validate() != nil {
					return ErrInvalidResponse
				}
				items = append(items, item)
			} else {
				for _, comb := range combinations {
					cid, e := comb.ID.Int64()
					if e != nil {
						return e
					}
					value, e := addMoney(p.Price, comb.Price)
					if e != nil {
						return e
					}
					cu, e := parsePSTime(comb.DateUpd)
					if e != nil {
						cu = updated
					}
					item := sdk.RemotePrice{VariantRemoteID: variantRemoteID(pid, cid), Value: value, Currency: cfg.StoreCurrency, UpdatedAt: cu}
					if item.Validate() != nil {
						return ErrInvalidResponse
					}
					items = append(items, item)
				}
			}
		}
		next, e := nextCursor(offset, request.Limit, len(products), cfg.fingerprint("prices"))
		if e != nil {
			return e
		}
		out = sdk.PricePage{Items: items, NextCursor: next}
		return out.Validate(50000)
	})
	return out, err
}

const storeLocationID = "prestashop-store"

func (c *Connector) ListInventoryLocations(ctx context.Context, account sdk.Account, runtime sdk.Runtime) ([]sdk.RemoteLocation, error) {
	if c == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	if _, err := c.configuration(ctx, account); err != nil {
		return nil, err
	}
	return []sdk.RemoteLocation{{RemoteID: storeLocationID, Name: "PrestaShop available stock"}}, nil
}
func (c *Connector) fetchStock(ctx context.Context, cfg Configuration, cred credentials, productID, attributeID int64) (psStock, error) {
	q := []QueryParam{{Name: "display", Value: "[id,id_product,id_product_attribute,quantity]"}, {Name: "filter[id_product]", Value: "[" + strconv.FormatInt(productID, 10) + "]"}, {Name: "filter[id_product_attribute]", Value: "[" + strconv.FormatInt(attributeID, 10) + "]"}, {Name: "limit", Value: "2"}}
	resp, err := c.call(ctx, cfg, cred, "GET", "/stock_availables", q, nil)
	if err != nil {
		return psStock{}, err
	}
	var env struct {
		Stocks []psStock `json:"stock_availables"`
	}
	if json.Unmarshal(resp.Body, &env) != nil || len(env.Stocks) != 1 {
		return psStock{}, ErrInvalidResponse
	}
	return env.Stocks[0], nil
}
func parseVariant(value string) (int64, int64, error) {
	var p, a int64
	if n, _ := fmtSscanf(value, "product:%d", &p); n == 1 && p > 0 {
		return p, 0, nil
	}
	if n, _ := fmtSscanf(value, "combination:%d:%d", &p, &a); n == 2 && p > 0 && a > 0 {
		return p, a, nil
	}
	return 0, 0, ErrInvalidResponse
}
func (c *Connector) ReadInventory(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.InventoryQuery) ([]sdk.RemoteInventory, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil || query.Validate(200) != nil || query.LocationRemoteID != storeLocationID {
		return nil, sdk.ErrInvalidReadRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return nil, err
	}
	out := []sdk.RemoteInventory{}
	err = c.withCredentials(ctx, runtime, account.SecretReference, func(cred credentials) error {
		for _, id := range query.VariantRemoteIDs {
			p, a, e := parseVariant(id)
			if e != nil {
				return sdk.ErrInvalidReadRequest
			}
			stock, e := c.fetchStock(ctx, cfg, cred, p, a)
			if e != nil {
				return e
			}
			qty, e := stock.Quantity.Int64()
			if e != nil {
				return e
			}
			item := sdk.RemoteInventory{LocationRemoteID: storeLocationID, VariantRemoteID: id, Quantity: qty}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			out = append(out, item)
		}
		return nil
	})
	return out, err
}

func (c *Connector) ReadOrders(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.OrderPage, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.read") != nil || request.Validate(100) != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.OrderPage{}, err
	}
	offset, err := decodePageCursor(request.Cursor, cfg.fingerprint("orders"))
	if err != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	var out sdk.OrderPage
	err = c.withCredentials(ctx, runtime, account.SecretReference, func(cred credentials) error {
		resp, e := c.call(ctx, cfg, cred, "GET", "/orders", []QueryParam{{Name: "display", Value: "[id,reference,current_state,date_add,date_upd]"}, {Name: "sort", Value: "[id_ASC]"}, {Name: "limit", Value: strconv.Itoa(offset) + "," + strconv.Itoa(request.Limit)}}, nil)
		if e != nil {
			return e
		}
		var env struct {
			Orders []psOrder `json:"orders"`
		}
		if json.Unmarshal(resp.Body, &env) != nil || len(env.Orders) > request.Limit {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteOrder, 0, len(env.Orders))
		for _, o := range env.Orders {
			oid, e := o.ID.Int64()
			if e != nil || oid < 1 {
				return ErrInvalidResponse
			}
			created, e := parsePSTime(o.DateAdd)
			if e != nil {
				return e
			}
			updated, e := parsePSTime(o.DateUpd)
			if e != nil || updated.Before(created) {
				return ErrInvalidResponse
			}
			status := o.CurrentState.String()
			if !validText(status, 64) {
				return ErrInvalidResponse
			}
			dresp, e := c.call(ctx, cfg, cred, "GET", "/order_details", []QueryParam{{Name: "display", Value: "[id,id_order,product_id,product_attribute_id,product_quantity]"}, {Name: "filter[id_order]", Value: "[" + strconv.FormatInt(oid, 10) + "]"}, {Name: "limit", Value: "1000"}}, nil)
			if e != nil {
				return e
			}
			var denv struct {
				Details []psOrderDetail `json:"order_details"`
			}
			if json.Unmarshal(dresp.Body, &denv) != nil || len(denv.Details) > 1000 {
				return ErrInvalidResponse
			}
			lines := make([]sdk.RemoteOrderItem, 0, len(denv.Details))
			for _, d := range denv.Details {
				did, e := d.ID.Int64()
				if e != nil {
					return e
				}
				pid, e := d.ProductID.Int64()
				if e != nil || pid < 1 {
					return ErrInvalidResponse
				}
				aid, e := d.AttributeID.Int64()
				if e != nil {
					return e
				}
				qty, e := d.Quantity.Int64()
				if e != nil || qty < 1 {
					return ErrInvalidResponse
				}
				line := sdk.RemoteOrderItem{RemoteID: strconv.FormatInt(did, 10), VariantRemoteID: variantRemoteID(pid, aid), Quantity: qty}
				if line.Validate() != nil {
					return ErrInvalidResponse
				}
				lines = append(lines, line)
			}
			item := sdk.RemoteOrder{RemoteID: strconv.FormatInt(oid, 10), ExternalID: o.Reference, StatusRemoteID: status, CreatedAt: created, UpdatedAt: updated, Items: lines}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, e := nextCursor(offset, request.Limit, len(env.Orders), cfg.fingerprint("orders"))
		if e != nil {
			return e
		}
		out = sdk.OrderPage{Items: items, NextCursor: next}
		return out.Validate(request.Limit)
	})
	return out, err
}
