package cscart

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func (c *Connector) listProducts(ctx context.Context, cfg Configuration, cred credentials, page, limit int) ([]csCartProduct, int, error) {
	query := []QueryParam{{Name: "page", Value: strconv.Itoa(page)}, {Name: "items_per_page", Value: strconv.Itoa(limit)}, {Name: "sort_by", Value: "product"}, {Name: "sort_order", Value: "asc"}, {Name: "pshort", Value: "Y"}, {Name: "pfull", Value: "Y"}}
	resp, err := c.call(ctx, cfg, cred, "GET", "/products", query, nil)
	if err != nil {
		return nil, 0, err
	}
	var envelope productListResponse
	if json.Unmarshal(resp.Body, &envelope) != nil || len(envelope.Products) > limit {
		return nil, 0, ErrInvalidResponse
	}
	total := 0
	if len(envelope.Params.TotalItems) > 0 {
		if value, parseErr := rawString(envelope.Params.TotalItems); parseErr == nil {
			total, _ = strconv.Atoi(value)
		}
	}
	return envelope.Products, total, nil
}

func (c *Connector) ReadProducts(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ProductPage, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || request.Validate(50) != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.ProductPage{}, err
	}
	fingerprint := cfg.fingerprint("products")
	page, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.ProductPage
	err = c.withCredentials(ctx, runtime, account.SecretReference, func(cred credentials) error {
		products, total, listErr := c.listProducts(ctx, cfg, cred, page, request.Limit)
		if listErr != nil {
			return listErr
		}
		items := make([]sdk.RemoteProduct, 0, len(products))
		for _, product := range products {
			id, idErr := productID(product)
			updated, timeErr := productUpdatedAt(product)
			if idErr != nil || timeErr != nil || !productValid(product) || !validRemoteText(product.ProductCode, 200) || !validRemoteText(product.Product, 500) {
				return ErrInvalidResponse
			}
			item := sdk.RemoteProduct{RemoteID: id, SellerSKU: product.ProductCode, Title: product.Product, UpdatedAt: updated, Variants: []sdk.RemoteVariant{{RemoteID: id, SKUs: []string{product.ProductCode}}}}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		if len(products) == request.Limit && (total == 0 || page*request.Limit < total) {
			cursor, cursorErr := encodePageCursor(page+1, fingerprint)
			if cursorErr != nil {
				return cursorErr
			}
			output.NextCursor = cursor
		}
		output.Items = items
		return output.Validate(request.Limit)
	})
	return output, err
}
