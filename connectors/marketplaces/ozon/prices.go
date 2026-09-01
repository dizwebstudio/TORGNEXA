package ozon

import (
	"context"
	"encoding/json"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type priceImportRequest struct {
	Prices []priceImportItem `json:"prices"`
}

type priceImportItem struct {
	OfferID      string `json:"offer_id"`
	Price        string `json:"price"`
	OldPrice     string `json:"old_price,omitempty"`
	CurrencyCode string `json:"currency_code"`
}

type priceImportResponse struct {
	Result []struct {
		OfferID string `json:"offer_id"`
		Updated bool   `json:"updated"`
		Errors  []struct {
			Code string `json:"code"`
		} `json:"errors"`
	} `json:"result"`
}

// WritePrice updates one Ozon offer through the Seller API import endpoint.
// Ozon returns a per-offer result, so an accepted HTTP response is not enough:
// the connector requires exactly one successful result for the requested
// offer. The host still reconciles the price because this endpoint does not
// return the canonical remote price projection.
func (connector *Connector) WritePrice(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.PriceWriteRequest) (sdk.CommerceWriteReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "prices.write") != nil || request.Validate() != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	body, err := json.Marshal(priceImportRequest{Prices: []priceImportItem{{
		OfferID: request.VariantRemoteID, Price: request.Value, OldPrice: request.CompareAt, CurrencyCode: request.Currency,
	}}})
	if err != nil {
		return sdk.CommerceWriteReceipt{}, sdk.ErrInvalidCommerceWrite
	}
	var receipt sdk.CommerceWriteReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(clientID, apiKey []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{
			Method:         "POST",
			Host:           apiHost,
			Path:           "/v1/product/import/prices",
			Body:           body,
			ClientID:       clientID,
			APIKey:         apiKey,
			IdempotencyKey: request.IdempotencyKey,
		})
		if callErr != nil {
			return writeOutcomeUnknown()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		var parsed priceImportResponse
		if len(response.Body) == 0 || len(response.Body) > maxBodyBytes || json.Unmarshal(response.Body, &parsed) != nil || len(parsed.Result) != 1 || parsed.Result[0].OfferID != request.VariantRemoteID {
			return ErrInvalidResponse
		}
		if !parsed.Result[0].Updated || len(parsed.Result[0].Errors) != 0 {
			remote, remoteErr := sdk.NewRemoteError(sdk.ErrorInvalidRequest, "remote_rejected", "", 0)
			if remoteErr != nil {
				return ErrInvalidResponse
			}
			return remote
		}
		receipt = sdk.CommerceWriteReceipt{RemoteID: request.VariantRemoteID, Applied: true}
		return receipt.Validate()
	})
	return receipt, err
}

var _ sdk.PriceWriter = (*Connector)(nil)
