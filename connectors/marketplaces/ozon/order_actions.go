package ozon

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type fbsOrderActionResponse struct {
	Result json.RawMessage `json:"result"`
}

// ApplyMarketplaceOrderAction implements the bounded Ozon FBS order commands
// that have an unambiguous Seller API contract. Confirmation is the pack/ship
// operation, handoff advances the seller handoff state, and cancellation
// requires the provider's numeric reason identifier. The operation is still
// reconciled by the host because Ozon documents ship as asynchronous.
func (connector *Connector) ApplyMarketplaceOrderAction(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.MarketplaceOrderActionRequest) (sdk.MarketplaceOperationReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.status.write") != nil || request.Validate() != nil {
		return sdk.MarketplaceOperationReceipt{}, sdk.ErrInvalidMarketplaceOperation
	}
	if request.DryRun {
		receipt := sdk.MarketplaceOperationReceipt{Status: sdk.MarketplaceOperationDryRun, RemoteID: request.OrderRemoteID, ObservedAt: connector.now().UTC()}
		return receipt, receipt.Validate()
	}

	path, body, err := ozonOrderAction(request)
	if err != nil {
		return sdk.MarketplaceOperationReceipt{}, err
	}
	var receipt sdk.MarketplaceOperationReceipt
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(clientID, apiKey []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: path, Body: body, ClientID: clientID, APIKey: apiKey, IdempotencyKey: request.IdempotencyKey})
		if callErr != nil {
			return writeOutcomeUnknown()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		if len(response.Body) == 0 || len(response.Body) > maxBodyBytes {
			return ErrInvalidResponse
		}
		var parsed fbsOrderActionResponse
		if json.Unmarshal(response.Body, &parsed) != nil || len(parsed.Result) == 0 || string(parsed.Result) == "null" {
			return ErrInvalidResponse
		}
		receipt = sdk.MarketplaceOperationReceipt{Status: sdk.MarketplaceOperationApplied, RemoteID: request.OrderRemoteID, ObservedAt: connector.now().UTC()}
		return receipt.Validate()
	})
	return receipt, err
}

func ozonOrderAction(request sdk.MarketplaceOrderActionRequest) (string, []byte, error) {
	switch request.Action {
	case sdk.MarketplaceOrderConfirm:
		return "/v4/posting/fbs/ship", mustJSON(ozonPostingNumberBody{PostingNumber: request.OrderRemoteID, With: ozonAdditionalData{AdditionalData: true}}), nil
	case sdk.MarketplaceOrderHandoff:
		return "/v2/fbs/posting/sent-by-seller", mustJSON(ozonPostingNumberBody{PostingNumber: request.OrderRemoteID}), nil
	case sdk.MarketplaceOrderCancel:
		reason, err := strconv.ParseInt(request.ReasonCode, 10, 32)
		if err != nil || reason < 1 {
			return "", nil, sdk.ErrInvalidMarketplaceOperation
		}
		return "/v2/posting/fbs/cancel", mustJSON(ozonCancelBody{PostingNumber: request.OrderRemoteID, CancelReasonID: reason}), nil
	default:
		return "", nil, sdk.ErrInvalidMarketplaceOperation
	}
}

type ozonPostingNumberBody struct {
	PostingNumber string             `json:"posting_number"`
	With          ozonAdditionalData `json:"with,omitempty"`
}

type ozonAdditionalData struct {
	AdditionalData bool `json:"additional_data"`
}

type ozonCancelBody struct {
	PostingNumber  string `json:"posting_number"`
	CancelReasonID int64  `json:"cancel_reason_id"`
}

func mustJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

var _ sdk.MarketplaceOrderActionWriter = (*Connector)(nil)
