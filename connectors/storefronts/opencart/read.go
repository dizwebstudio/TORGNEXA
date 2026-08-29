package opencart

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type productEnvelope struct {
	Items      []bridgeProduct `json:"items"`
	Page       int             `json:"page"`
	TotalPages int             `json:"total_pages"`
}

func (c *Connector) listProducts(ctx context.Context, cfg Configuration, cred credentials, page, limit int) (productEnvelope, error) {
	resp, e := c.call(ctx, cfg, cred, "GET", "products", []QueryParam{{Name: "page", Value: strconv.Itoa(page)}, {Name: "limit", Value: strconv.Itoa(limit)}}, nil)
	if e != nil {
		return productEnvelope{}, e
	}
	var env productEnvelope
	if json.Unmarshal(resp.Body, &env) != nil || env.Page != page || env.TotalPages < 1 || len(env.Items) > limit {
		return productEnvelope{}, ErrInvalidResponse
	}
	return env, nil
}
func projectVariants(p bridgeProduct) ([]sdk.RemoteVariant, string, error) {
	if p.ID < 1 {
		return nil, "", ErrInvalidResponse
	}
	if len(p.Variants) == 0 {
		if !validText(p.SKU, 200) {
			return nil, "", ErrInvalidResponse
		}
		return []sdk.RemoteVariant{{RemoteID: productVariantID(p.ID), SKUs: []string{p.SKU}}}, p.SKU, nil
	}
	if len(p.Variants) > 1000 {
		return nil, "", ErrInvalidResponse
	}
	out := make([]sdk.RemoteVariant, 0, len(p.Variants))
	sku := p.SKU
	for _, v := range p.Variants {
		if !validText(v.RemoteID, 300) || !validText(v.SKU, 200) {
			return nil, "", ErrInvalidResponse
		}
		if sku == "" {
			sku = v.SKU
		}
		out = append(out, sdk.RemoteVariant{RemoteID: v.RemoteID, SKUs: []string{v.SKU}})
	}
	if !validText(sku, 200) {
		return nil, "", ErrInvalidResponse
	}
	return out, sku, nil
}
func (c *Connector) ReadProducts(ctx context.Context, a sdk.Account, r sdk.Runtime, req sdk.PageRequest) (sdk.ProductPage, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || req.Validate(100) != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.ProductPage{}, e
	}
	page, e := decodePageCursor(req.Cursor, cfg.fingerprint("products"))
	if e != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	var out sdk.ProductPage
	e = c.withCredentials(ctx, r, a.SecretReference, func(cred credentials) error {
		env, e := c.listProducts(ctx, cfg, cred, page, req.Limit)
		if e != nil {
			return e
		}
		items := make([]sdk.RemoteProduct, 0, len(env.Items))
		for _, p := range env.Items {
			if p.ID < 1 || !validText(p.Title, 500) {
				return ErrInvalidResponse
			}
			updated, e := parseTime(p.ModifiedAt)
			if e != nil {
				return e
			}
			variants, sku, e := projectVariants(p)
			if e != nil {
				return e
			}
			item := sdk.RemoteProduct{RemoteID: strconv.FormatInt(p.ID, 10), SellerSKU: sku, Title: p.Title, Brand: p.Brand, UpdatedAt: updated, Variants: variants}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, e := nextCursor(page, env.TotalPages, cfg.fingerprint("products"))
		if e != nil {
			return e
		}
		out = sdk.ProductPage{Items: items, NextCursor: next}
		return out.Validate(req.Limit)
	})
	return out, e
}
func (c *Connector) ReadPrices(ctx context.Context, a sdk.Account, r sdk.Runtime, req sdk.PageRequest) (sdk.PricePage, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.read") != nil || req.Validate(100) != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.PricePage{}, e
	}
	page, e := decodePageCursor(req.Cursor, cfg.fingerprint("prices"))
	if e != nil {
		return sdk.PricePage{}, sdk.ErrInvalidReadRequest
	}
	var out sdk.PricePage
	e = c.withCredentials(ctx, r, a.SecretReference, func(cred credentials) error {
		env, e := c.listProducts(ctx, cfg, cred, page, req.Limit)
		if e != nil {
			return e
		}
		items := []sdk.RemotePrice{}
		for _, p := range env.Items {
			updated, e := parseTime(p.ModifiedAt)
			if e != nil {
				return e
			}
			if len(p.Variants) == 0 {
				item := sdk.RemotePrice{VariantRemoteID: productVariantID(p.ID), Value: p.Price, CompareAt: p.CompareAt, Currency: cfg.StoreCurrency, UpdatedAt: updated}
				if item.Validate() != nil {
					return ErrInvalidResponse
				}
				items = append(items, item)
			} else {
				for _, v := range p.Variants {
					vu, e := parseTime(v.ModifiedAt)
					if e != nil {
						vu = updated
					}
					item := sdk.RemotePrice{VariantRemoteID: v.RemoteID, Value: v.Price, CompareAt: v.CompareAt, Currency: cfg.StoreCurrency, UpdatedAt: vu}
					if item.Validate() != nil {
						return ErrInvalidResponse
					}
					items = append(items, item)
				}
			}
		}
		next, e := nextCursor(page, env.TotalPages, cfg.fingerprint("prices"))
		if e != nil {
			return e
		}
		out = sdk.PricePage{Items: items, NextCursor: next}
		return out.Validate(100000)
	})
	return out, e
}

const storeLocationID = "opencart-store"

func (c *Connector) ListInventoryLocations(ctx context.Context, a sdk.Account, r sdk.Runtime) ([]sdk.RemoteLocation, error) {
	if c == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil {
		return nil, sdk.ErrInvalidReadRequest
	}
	if _, e := c.configuration(ctx, a); e != nil {
		return nil, e
	}
	return []sdk.RemoteLocation{{RemoteID: storeLocationID, Name: "OpenCart store stock"}}, nil
}

type variantState struct {
	RemoteID   string `json:"remote_id"`
	Price      string `json:"price"`
	CompareAt  string `json:"compare_at"`
	Quantity   int64  `json:"quantity"`
	ModifiedAt string `json:"modified_at"`
}

func (c *Connector) fetchVariant(ctx context.Context, cfg Configuration, cred credentials, id string) (variantState, error) {
	resp, e := c.call(ctx, cfg, cred, "GET", "variant", []QueryParam{{Name: "remote_id", Value: id}}, nil)
	if e != nil {
		return variantState{}, e
	}
	var v variantState
	if json.Unmarshal(resp.Body, &v) != nil || v.RemoteID != id || v.Quantity < 0 {
		return variantState{}, ErrInvalidResponse
	}
	return v, nil
}
func (c *Connector) ReadInventory(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.InventoryQuery) ([]sdk.RemoteInventory, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "inventory.read") != nil || q.Validate(200) != nil || q.LocationRemoteID != storeLocationID {
		return nil, sdk.ErrInvalidReadRequest
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return nil, e
	}
	out := []sdk.RemoteInventory{}
	e = c.withCredentials(ctx, r, a.SecretReference, func(cred credentials) error {
		for _, id := range q.VariantRemoteIDs {
			v, e := c.fetchVariant(ctx, cfg, cred, id)
			if e != nil {
				return e
			}
			item := sdk.RemoteInventory{LocationRemoteID: storeLocationID, VariantRemoteID: id, Quantity: v.Quantity}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			out = append(out, item)
		}
		return nil
	})
	return out, e
}

type orderEnvelope struct {
	Items      []bridgeOrder `json:"items"`
	Page       int           `json:"page"`
	TotalPages int           `json:"total_pages"`
}

func (c *Connector) ReadOrders(ctx context.Context, a sdk.Account, r sdk.Runtime, req sdk.PageRequest) (sdk.OrderPage, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.read") != nil || req.Validate(100) != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.OrderPage{}, e
	}
	page, e := decodePageCursor(req.Cursor, cfg.fingerprint("orders"))
	if e != nil {
		return sdk.OrderPage{}, sdk.ErrInvalidReadRequest
	}
	var out sdk.OrderPage
	e = c.withCredentials(ctx, r, a.SecretReference, func(cred credentials) error {
		resp, e := c.call(ctx, cfg, cred, "GET", "orders", []QueryParam{{Name: "page", Value: strconv.Itoa(page)}, {Name: "limit", Value: strconv.Itoa(req.Limit)}}, nil)
		if e != nil {
			return e
		}
		var env orderEnvelope
		if json.Unmarshal(resp.Body, &env) != nil || env.Page != page || env.TotalPages < 1 || len(env.Items) > req.Limit {
			return ErrInvalidResponse
		}
		items := make([]sdk.RemoteOrder, 0, len(env.Items))
		for _, o := range env.Items {
			created, e := parseTime(o.CreatedAt)
			if e != nil {
				return e
			}
			updated, e := parseTime(o.UpdatedAt)
			if e != nil || updated.Before(created) || o.ID < 1 || !validText(o.StatusRemoteID, 64) {
				return ErrInvalidResponse
			}
			lines := make([]sdk.RemoteOrderItem, 0, len(o.Items))
			for _, l := range o.Items {
				line := sdk.RemoteOrderItem{RemoteID: strconv.FormatInt(l.ID, 10), VariantRemoteID: l.VariantRemoteID, Quantity: l.Quantity}
				if line.Validate() != nil {
					return ErrInvalidResponse
				}
				lines = append(lines, line)
			}
			item := sdk.RemoteOrder{RemoteID: strconv.FormatInt(o.ID, 10), ExternalID: o.ExternalID, StatusRemoteID: o.StatusRemoteID, CreatedAt: created, UpdatedAt: updated, Items: lines}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		next, e := nextCursor(page, env.TotalPages, cfg.fingerprint("orders"))
		if e != nil {
			return e
		}
		out = sdk.OrderPage{Items: items, NextCursor: next}
		return out.Validate(req.Limit)
	})
	return out, e
}
