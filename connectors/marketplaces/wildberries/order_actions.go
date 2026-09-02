package wildberries

import (
	"context"
	"strconv"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// ApplyMarketplaceOrderAction implements the unambiguous WB FBS seller
// cancellation command. WB exposes cancellation as a dedicated PATCH and
// does not expose a generic seller-side confirmation transition for this
// assembly-order projection; unsupported actions fail closed.
func (connector *Connector) ApplyMarketplaceOrderAction(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.MarketplaceOrderActionRequest) (sdk.MarketplaceOperationReceipt, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "orders.status.write") != nil || request.Validate() != nil {
		return sdk.MarketplaceOperationReceipt{}, sdk.ErrInvalidMarketplaceOperation
	}
	if request.Action != sdk.MarketplaceOrderCancel {
		return sdk.MarketplaceOperationReceipt{}, sdk.ErrInvalidMarketplaceOperation
	}
	orderID, err := strconv.ParseInt(request.OrderRemoteID, 10, 64)
	if err != nil || orderID < 1 {
		return sdk.MarketplaceOperationReceipt{}, sdk.ErrInvalidMarketplaceOperation
	}
	if request.DryRun {
		receipt := sdk.MarketplaceOperationReceipt{Status: sdk.MarketplaceOperationDryRun, RemoteID: request.OrderRemoteID, ObservedAt: connector.now().UTC()}
		return receipt, receipt.Validate()
	}
	var receipt sdk.MarketplaceOperationReceipt
	err = runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "PATCH", Host: marketplaceHost, Path: "/api/v3/orders/" + strconv.FormatInt(orderID, 10) + "/cancel", Token: secret, IdempotencyKey: request.IdempotencyKey})
		if callErr != nil {
			return writeOutcomeUnknown()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		receipt = sdk.MarketplaceOperationReceipt{Status: sdk.MarketplaceOperationApplied, RemoteID: request.OrderRemoteID, ObservedAt: connector.now().UTC()}
		return receipt.Validate()
	})
	return receipt, err
}

var _ sdk.MarketplaceOrderActionWriter = (*Connector)(nil)
