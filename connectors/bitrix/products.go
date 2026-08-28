package bitrix

import (
	"context"
	"encoding/json"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func (connector *Connector) listProducts(ctx context.Context, configuration Configuration, credential credentials, offset int) ([]bitrixProduct, int, error) {
	response, err := connector.call(ctx, configuration, credential, "catalog.product.list", map[string]any{
		"select": []string{"id", "iblockId", "name", "active", "code", "xmlId", "detailText", "quantity", "timestampX", "dateCreate"},
		"filter": map[string]any{"iblockId": configuration.CatalogIblockID},
		"order":  map[string]string{"id": "asc"},
		"start":  offset,
	})
	if err != nil {
		return nil, 0, err
	}
	products, err := decodeProductList(response.Body)
	if err != nil {
		return nil, 0, err
	}
	return products, offset + len(products), nil
}

func (connector *Connector) ReadProducts(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PageRequest) (sdk.ProductPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || request.Validate(50) != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.ProductPage{}, err
	}
	fingerprint := configuration.fingerprint("products")
	offset, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.ProductPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		products, nextOffset, listErr := connector.listProducts(ctx, configuration, credential, offset)
		if listErr != nil {
			return listErr
		}
		items := make([]sdk.RemoteProduct, 0, len(products))
		for _, product := range products {
			if product.ID < 1 || product.IblockID != configuration.CatalogIblockID || !validRemoteText(product.Name, 500) || !validRemoteText(productSKU(product), 200) {
				return ErrInvalidResponse
			}
			updated, timeErr := productUpdatedAt(product)
			if timeErr != nil {
				return ErrInvalidResponse
			}
			remoteID := intString(product.ID)
			item := sdk.RemoteProduct{RemoteID: remoteID, SellerSKU: productSKU(product), Title: product.Name, UpdatedAt: updated, Variants: []sdk.RemoteVariant{{RemoteID: remoteID, SKUs: []string{productSKU(product)}}}}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			items = append(items, item)
		}
		if len(products) == 50 {
			cursor, cursorErr := encodePageCursor(nextOffset, fingerprint)
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

func (connector *Connector) findProductBySKU(ctx context.Context, configuration Configuration, credential credentials, sku string) (bitrixProduct, error) {
	response, err := connector.call(ctx, configuration, credential, "catalog.product.list", map[string]any{
		"select": []string{"id", "iblockId", "name", "active", "code", "xmlId", "detailText", "quantity", "timestampX", "dateCreate"},
		"filter": map[string]any{"iblockId": configuration.CatalogIblockID, "xmlId": sku},
		"order":  map[string]string{"id": "asc"},
		"start":  0,
	})
	if err != nil {
		return bitrixProduct{}, err
	}
	products, err := decodeProductList(response.Body)
	if err != nil || len(products) > 1 {
		return bitrixProduct{}, ErrInvalidResponse
	}
	if len(products) == 0 {
		return bitrixProduct{}, ErrProductNotFound
	}
	return products[0], nil
}

func (connector *Connector) fetchProduct(ctx context.Context, configuration Configuration, credential credentials, id int64) (bitrixProduct, error) {
	response, err := connector.call(ctx, configuration, credential, "catalog.product.get", map[string]any{"id": id})
	if err != nil {
		return bitrixProduct{}, err
	}
	var envelope struct {
		Result struct {
			Product bitrixProduct `json:"product"`
		} `json:"result"`
	}
	if json.Unmarshal(response.Body, &envelope) != nil || envelope.Result.Product.ID != id || envelope.Result.Product.IblockID != configuration.CatalogIblockID {
		return bitrixProduct{}, ErrInvalidResponse
	}
	return envelope.Result.Product, nil
}
