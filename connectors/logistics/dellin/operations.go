package dellin

import (
	"context"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// ReadPickupPoints reads a bounded Деловые Линии terminal/PUDO directory.
// Provider identifiers stay remote references and never become Core warehouse
// identifiers.
func (c *Connector) ReadPickupPoints(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || query.Validate(500) != nil {
		return nil, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out []sdk.PickupPoint
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.Pickup(ctx, secret, query)
		return callErr
	})
	if err != nil {
		return nil, err
	}
	for _, point := range out {
		if point.Validate() != nil {
			return nil, remote(sdk.ErrorInternal, "invalid_remote_response")
		}
	}
	return out, nil
}

// ReadLogisticsTracking reads one order status history through the official
// Деловые Линии public API.
func (c *Connector) ReadLogisticsTracking(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || strings.TrimSpace(request.RemoteID) == "" {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out sdk.ShipmentResult
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.Track(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if out.RemoteID == "" || out.Status == "" || out.Cost.Validate() != nil || out.ObservedAt.IsZero() {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	return out, nil
}

func useSecret(ctx context.Context, runtime sdk.Runtime, account sdk.Account, fn func([]byte) error) error {
	if runtime == nil || runtime.Secrets() == nil {
		return remote(sdk.ErrorUnauthorized, "credential_missing")
	}
	return runtime.Secrets().UseSecret(ctx, account.SecretReference, fn)
}

var _ sdk.PickupPointReader = (*Connector)(nil)
var _ sdk.LogisticsTracker = (*Connector)(nil)
