package shopware

import (
	"context"
	"encoding/json"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// searchCriteria builds a Shopware Criteria API request body
// (https://developer.shopware.com/docs/guides/integrations-api/general-concepts/search-criteria.html).
func searchCriteria(page, limit int, filter []map[string]any) []byte {
	body, _ := json.Marshal(map[string]any{"page": page, "limit": limit, "sort": []map[string]any{{"field": "updatedAt", "order": "ASC"}}, "filter": filter})
	return body
}

func (connector *Connector) listTopLevelProducts(ctx context.Context, configuration Configuration, accountID string, credential credentials, page, limit int) ([]shopwareProduct, int, error) {
	body := searchCriteria(page, limit, []map[string]any{{"type": "equals", "field": "parentId", "value": nil}})
	response, err := connector.call(ctx, configuration, accountID, credential, "POST", "/search/product", nil, body)
	if err != nil {
		return nil, 0, err
	}
	var page_ shopwareSearchPage[shopwareProduct]
	if json.Unmarshal(response.Body, &page_) != nil || len(page_.Data) > limit {
		return nil, 0, ErrInvalidResponse
	}
	return page_.Data, page_.Total, nil
}

func (connector *Connector) listVariants(ctx context.Context, configuration Configuration, accountID string, credential credentials, parentID string) ([]shopwareProduct, error) {
	var all []shopwareProduct
	for page := 1; page <= 10; page++ {
		body := searchCriteria(page, 100, []map[string]any{{"type": "equals", "field": "parentId", "value": parentID}})
		response, err := connector.call(ctx, configuration, accountID, credential, "POST", "/search/product", nil, body)
		if err != nil {
			return nil, err
		}
		var result shopwareSearchPage[shopwareProduct]
		if json.Unmarshal(response.Body, &result) != nil || len(result.Data) > 100 {
			return nil, ErrInvalidResponse
		}
		all = append(all, result.Data...)
		if len(all) > 1000 {
			return nil, ErrInvalidResponse
		}
		if page*100 >= result.Total {
			break
		}
		if page == 10 {
			return nil, ErrInvalidResponse
		}
	}
	return all, nil
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
	page, err := decodePageCursor(request.Cursor, fingerprint)
	if err != nil {
		return sdk.ProductPage{}, sdk.ErrInvalidReadRequest
	}
	var output sdk.ProductPage
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		products, total, callErr := connector.listTopLevelProducts(ctx, configuration, account.ID, credential, page, request.Limit)
		if callErr != nil {
			return callErr
		}
		result := make([]sdk.RemoteProduct, 0, len(products))
		for _, product := range products {
			if product.ID == "" || !validRemoteText(product.Name, 500) || !validRemoteText(product.ProductNumber, 200) {
				return ErrInvalidResponse
			}
			updated, parseErr := time.Parse(time.RFC3339, product.UpdatedAt)
			if parseErr != nil {
				return ErrInvalidResponse
			}
			var variants []sdk.RemoteVariant
			if product.ChildCount > 0 {
				children, variantErr := connector.listVariants(ctx, configuration, account.ID, credential, product.ID)
				if variantErr != nil {
					return variantErr
				}
				for _, child := range children {
					if child.ID == "" || !validRemoteText(child.ProductNumber, 200) {
						return ErrInvalidResponse
					}
					variants = append(variants, sdk.RemoteVariant{RemoteID: child.ID, SKUs: []string{child.ProductNumber}})
				}
			}
			if len(variants) == 0 {
				variants = append(variants, sdk.RemoteVariant{RemoteID: product.ID, SKUs: []string{product.ProductNumber}})
			}
			item := sdk.RemoteProduct{RemoteID: product.ID, SellerSKU: product.ProductNumber, Title: product.Name, UpdatedAt: updated.UTC(), Variants: variants}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			result = append(result, item)
		}
		next, nextErr := nextCursor(page, request.Limit, total, fingerprint)
		if nextErr != nil {
			return nextErr
		}
		output = sdk.ProductPage{Items: result, NextCursor: next}
		return output.Validate(request.Limit)
	})
	return output, err
}
