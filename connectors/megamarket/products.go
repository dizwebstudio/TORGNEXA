package megamarket

import (
	"context"
	"encoding/json"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type cardsResponse struct {
	Success bool `json:"success"`
	Data    *struct {
		Items []struct {
			GoodsID   string   `json:"goodsId"`
			OfferID   string   `json:"offerId"`
			Name      string   `json:"name"`
			Brand     string   `json:"brand"`
			UpdatedAt string   `json:"updatedAt"`
			Barcodes  []string `json:"barcodes"`
		} `json:"items"`
		SearchAfter string `json:"searchAfter"`
	} `json:"data"`
}

func (c *Connector) ReadProducts(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.PageRequest) (sdk.ProductPage, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || q.Validate(100) != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.ProductPage{}, e
	}
	token, e := parseTokenCursor(q.Cursor, cfg.fingerprint("products"))
	if e != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	body, _ := json.Marshal(map[string]any{"filter": map[string]any{}, "sorting": map[string]any{"field": "offerId", "order": "asc"}, "searchAfter": token, "limit": q.Limit})
	var out sdk.ProductPage
	e = c.withToken(ctx, r, a.SecretReference, func(k []byte) error {
		resp, ce := c.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/api/merchantIntegration/assortment/v1/card/getAttributes", Body: body, MerchantToken: k})
		if ce != nil {
			return normalizedTransportError()
		}
		if x := normalizeHTTP(resp); x != nil {
			return x
		}
		p, pe := parseProducts(resp.Body, q.Limit, cfg, token)
		out = p
		return pe
	})
	return out, e
}
func parseProducts(body []byte, limit int, cfg Configuration, prev string) (sdk.ProductPage, error) {
	var x cardsResponse
	if len(body) == 0 || len(body) > maxBodyBytes || json.Unmarshal(body, &x) != nil || !x.Success || x.Data == nil || len(x.Data.Items) > limit {
		return sdk.ProductPage{}, ErrInvalidResponse
	}
	if x.Data.SearchAfter != "" && (x.Data.SearchAfter == prev || !validTokenText(x.Data.SearchAfter)) {
		return sdk.ProductPage{}, ErrInvalidResponse
	}
	items := make([]sdk.RemoteProduct, 0, len(x.Data.Items))
	seen := map[string]struct{}{}
	for _, v := range x.Data.Items {
		if !validText(v.GoodsID, 128) || !validText(v.OfferID, 200) || !validText(v.Name, 500) || !validOptionalText(v.Brand, 300) || len(v.Barcodes) > 100 {
			return sdk.ProductPage{}, ErrInvalidResponse
		}
		if _, ok := seen[v.GoodsID]; ok {
			return sdk.ProductPage{}, ErrInvalidResponse
		}
		seen[v.GoodsID] = struct{}{}
		t, e := parseUTC(v.UpdatedAt)
		if e != nil {
			return sdk.ProductPage{}, e
		}
		skus := []string{v.OfferID}
		for _, b := range v.Barcodes {
			if !validText(b, 200) {
				return sdk.ProductPage{}, ErrInvalidResponse
			}
			skus = append(skus, b)
		}
		p := sdk.RemoteProduct{RemoteID: v.GoodsID, SellerSKU: v.OfferID, Title: v.Name, Brand: v.Brand, UpdatedAt: t, Variants: []sdk.RemoteVariant{{RemoteID: v.OfferID, SKUs: skus}}}
		if p.Validate() != nil {
			return sdk.ProductPage{}, ErrInvalidResponse
		}
		items = append(items, p)
	}
	next, e := makeTokenCursor(x.Data.SearchAfter, cfg.fingerprint("products"))
	if e != nil {
		return sdk.ProductPage{}, e
	}
	p := sdk.ProductPage{Items: items, NextCursor: next}
	if p.Validate(limit) != nil {
		return sdk.ProductPage{}, ErrInvalidResponse
	}
	return p, nil
}
