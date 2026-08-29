package cscart

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func productMatches(product csCartProduct, request sdk.ProductWriteRequest) bool {
	return product.ProductCode == request.SellerSKU && product.Product == request.Title && product.Status == request.StatusRemoteID && product.FullDescription == request.Description
}

func (c *Connector) findProductBySKU(ctx context.Context, cfg Configuration, cred credentials, sku string) (csCartProduct, error) {
	resp, err := c.call(ctx, cfg, cred, "GET", "/products", []QueryParam{{Name: "pcode", Value: sku}, {Name: "items_per_page", Value: "2"}, {Name: "pshort", Value: "Y"}, {Name: "pfull", Value: "Y"}}, nil)
	if err != nil {
		return csCartProduct{}, err
	}
	var envelope productListResponse
	if json.Unmarshal(resp.Body, &envelope) != nil || len(envelope.Products) > 1 {
		return csCartProduct{}, ErrInvalidResponse
	}
	if len(envelope.Products) == 0 {
		return csCartProduct{}, ErrProductNotFound
	}
	product := envelope.Products[0]
	// The pcode filter is remote input, not an integrity guarantee. Refuse to
	// update a different product if a store/plugin returns an inconsistent
	// response; silently retargeting a SKU would violate the write contract.
	if product.ProductCode != sku || !productValid(product) || !validRemoteText(product.Product, 500) || !validRemoteText(product.ProductCode, 200) {
		return csCartProduct{}, ErrInvalidResponse
	}
	if _, err := productID(product); err != nil {
		return csCartProduct{}, err
	}
	return product, nil
}

func (c *Connector) fetchProduct(ctx context.Context, cfg Configuration, cred credentials, id string) (csCartProduct, error) {
	if parsed, err := strconv.ParseInt(id, 10, 64); err != nil || parsed < 1 {
		return csCartProduct{}, ErrInvalidResponse
	}
	resp, err := c.call(ctx, cfg, cred, "GET", "/products/"+id, nil, nil)
	if err != nil {
		return csCartProduct{}, err
	}
	var product csCartProduct
	if json.Unmarshal(resp.Body, &product) != nil {
		return csCartProduct{}, ErrInvalidResponse
	}
	remoteID, idErr := productID(product)
	if idErr != nil || remoteID != id {
		return csCartProduct{}, ErrInvalidResponse
	}
	return product, nil
}

func (c *Connector) UpsertProduct(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ProductWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	if request.StatusRemoteID != "A" && request.StatusRemoteID != "D" && request.StatusRemoteID != "H" {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = c.withCredentials(ctx, runtime, account.SecretReference, func(cred credentials) error {
		var current csCartProduct
		if request.RemoteID != "" {
			current, err = c.fetchProduct(ctx, cfg, cred, request.RemoteID)
		} else {
			current, err = c.findProductBySKU(ctx, cfg, cred, request.SellerSKU)
		}
		if err == nil {
			id, idErr := productID(current)
			if idErr != nil {
				return idErr
			}
			if productMatches(current, request) {
				receipt = sdk.CommerceWriteReceipt{RemoteID: id, Duplicate: true, Reconciled: true}
				return receipt.Validate()
			}
			body, _ := json.Marshal(map[string]string{"product": request.Title, "product_code": request.SellerSKU, "full_description": request.Description, "status": request.StatusRemoteID})
			if _, callErr := c.call(ctx, cfg, cred, "PUT", "/products/"+id, nil, body); callErr != nil {
				if !isAmbiguousWrite(callErr) {
					return callErr
				}
				updated, reconcileErr := c.fetchProduct(ctx, cfg, cred, id)
				if reconcileErr == nil && productMatches(updated, request) {
					receipt = sdk.CommerceWriteReceipt{RemoteID: id, Applied: true, Reconciled: true}
					return receipt.Validate()
				}
				return writeOutcomeUnknown()
			}
			updated, fetchErr := c.fetchProduct(ctx, cfg, cred, id)
			if fetchErr != nil || !productMatches(updated, request) {
				return ErrInvalidResponse
			}
			receipt = sdk.CommerceWriteReceipt{RemoteID: id, Applied: true}
			return receipt.Validate()
		}
		if request.RemoteID != "" || !errors.Is(err, ErrProductNotFound) {
			return err
		}
		body, _ := json.Marshal(map[string]string{"product": request.Title, "product_code": request.SellerSKU, "full_description": request.Description, "status": request.StatusRemoteID})
		resp, addErr := c.call(ctx, cfg, cred, "POST", "/products", nil, body)
		if addErr != nil {
			return addErr
		}
		var created struct {
			ProductID json.RawMessage `json:"product_id"`
		}
		if json.Unmarshal(resp.Body, &created) != nil {
			return ErrInvalidResponse
		}
		id, idErr := rawString(created.ProductID)
		if idErr != nil {
			return idErr
		}
		createdProduct, fetchErr := c.fetchProduct(ctx, cfg, cred, id)
		if fetchErr != nil || !productMatches(createdProduct, request) {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: id, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}

func validRemoteText(value string, max int) bool {
	if value == "" || value != strings.TrimSpace(value) || utf8.RuneCountInString(value) > max {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
