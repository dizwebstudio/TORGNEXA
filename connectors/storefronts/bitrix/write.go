package bitrix

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func productMatches(product bitrixProduct, request sdk.ProductWriteRequest, active string) bool {
	return product.ID > 0 && product.Name == request.Title && product.Active == active && productSKU(product) == request.SellerSKU && product.DetailText == request.Description
}

func productFields(configuration Configuration, request sdk.ProductWriteRequest, active string) map[string]any {
	return map[string]any{
		"iblockId":   configuration.CatalogIblockID,
		"name":       request.Title,
		"active":     active,
		"xmlId":      request.SellerSKU,
		"detailText": request.Description,
	}
}

// UpsertProduct uses catalog.product.add/update from the official Bitrix
// catalog REST surface. New products require catalog_iblock_id in runtime
// configuration; an existing xmlId is reused to keep retries idempotent.
func (connector *Connector) UpsertProduct(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ProductWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	active, ok := activeValue(request.StatusRemoteID)
	if !ok {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWriteReceipt{}, err
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		var current bitrixProduct
		if request.RemoteID != "" {
			id, parseErr := strconv.ParseInt(request.RemoteID, 10, 64)
			if parseErr != nil || id < 1 {
				return sdk.ErrInvalidCommerceWrite
			}
			current, err = connector.fetchProduct(ctx, configuration, credential, id)
		} else {
			current, err = connector.findProductBySKU(ctx, configuration, credential, request.SellerSKU)
		}
		if err == nil {
			if productMatches(current, request, active) {
				receipt = sdk.CommerceWriteReceipt{RemoteID: intString(current.ID), Duplicate: true, Reconciled: true}
				return receipt.Validate()
			}
			body := map[string]any{"id": current.ID, "fields": productFields(configuration, request, active)}
			if _, callErr := connector.call(ctx, configuration, credential, "catalog.product.update", body); callErr != nil {
				if !isAmbiguousWrite(callErr) {
					return callErr
				}
				updated, reconcileErr := connector.fetchProduct(ctx, configuration, credential, current.ID)
				if reconcileErr == nil && productMatches(updated, request, active) {
					receipt = sdk.CommerceWriteReceipt{RemoteID: intString(current.ID), Applied: true, Reconciled: true}
					return receipt.Validate()
				}
				return writeOutcomeUnknown()
			}
			updated, fetchErr := connector.fetchProduct(ctx, configuration, credential, current.ID)
			if fetchErr != nil || !productMatches(updated, request, active) {
				return ErrInvalidResponse
			}
			receipt = sdk.CommerceWriteReceipt{RemoteID: intString(current.ID), Applied: true}
			return receipt.Validate()
		}
		if request.RemoteID != "" || !errors.Is(err, ErrProductNotFound) {
			return err
		}
		response, addErr := connector.call(ctx, configuration, credential, "catalog.product.add", map[string]any{"fields": productFields(configuration, request, active)})
		if addErr != nil {
			return addErr
		}
		var envelope struct {
			Result struct {
				Element bitrixProduct `json:"element"`
				Product bitrixProduct `json:"product"`
				ID      json.Number   `json:"id"`
			} `json:"result"`
		}
		if json.Unmarshal(response.Body, &envelope) != nil {
			return ErrInvalidResponse
		}
		id := envelope.Result.Element.ID
		if id < 1 {
			id = envelope.Result.Product.ID
		}
		if id < 1 && envelope.Result.ID != "" {
			id, _ = envelope.Result.ID.Int64()
		}
		if id < 1 {
			return ErrInvalidResponse
		}
		created, fetchErr := connector.fetchProduct(ctx, configuration, credential, id)
		if fetchErr != nil || !productMatches(created, request, active) {
			return ErrInvalidResponse
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: intString(id), Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}
