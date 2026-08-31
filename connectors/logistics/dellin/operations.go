package dellin

import (
	"context"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// CreateLogisticsShipment places a bounded address-to-address request through
// the official Деловые Линии API. Account configuration supplies provider
// identifiers that cannot be inferred from the neutral SDK request.
func (c *Connector) CreateLogisticsShipment(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil || request.PickupPointRef != "" {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	configuration, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if configuration.Validate() != nil {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "configuration_rejected")
	}
	var result sdk.ShipmentResult
	err = useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		result, callErr = c.transport.Create(ctx, secret, request, configuration)
		return callErr
	})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if result.RemoteID == "" || result.Status == "" || result.Cost.Validate() != nil || result.ObservedAt.IsZero() {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	return result, nil
}

// CancelLogisticsShipment submits an address-delivery cancellation request to
// the official Деловые Линии API. The provider acknowledges receipt first and
// decides the cancellation asynchronously, so the normalized result is
// cancellation_pending rather than a false terminal cancellation.
func (c *Connector) CancelLogisticsShipment(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || strings.TrimSpace(request.RemoteID) == "" || strings.TrimSpace(request.IdempotencyKey) == "" {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var result sdk.ShipmentResult
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		result, callErr = c.transport.Cancel(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if result.RemoteID != strings.TrimSpace(request.RemoteID) || result.Status != "cancellation_pending" || result.Cost.Validate() != nil || result.ObservedAt.IsZero() {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInternal, "invalid_remote_response")
	}
	return result, nil
}

// ReadLogisticsRates calculates a bounded delivery-rate preview through the
// official Деловые Линии calculator.
func (c *Connector) ReadLogisticsRates(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.RateRequest) ([]sdk.RateQuote, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil {
		return nil, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var result []sdk.RateQuote
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		result, callErr = c.transport.Rates(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return nil, err
	}
	for _, quote := range result {
		if quote.Validate() != nil {
			return nil, remote(sdk.ErrorInternal, "invalid_remote_response")
		}
	}
	return result, nil
}

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

// ReadLogisticsLabel requests the official PDF waybill form. The document UID
// is supplied explicitly because the create response contains a request ID,
// while Деловые Линии printable forms are keyed by the waybill document UID.
func (c *Connector) ReadLogisticsLabel(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.LabelRequest) (sdk.LabelResult, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil {
		return sdk.LabelResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out sdk.LabelResult
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.Label(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.LabelResult{}, err
	}
	if out.ArtifactRef == "" || out.MediaType != "application/pdf" || out.ObservedAt.IsZero() {
		return sdk.LabelResult{}, remote(sdk.ErrorInternal, "invalid_remote_response")
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
var _ sdk.LogisticsShipmentCreator = (*Connector)(nil)
var _ sdk.LogisticsShipmentCanceler = (*Connector)(nil)
var _ sdk.LogisticsRateReader = (*Connector)(nil)
var _ sdk.LogisticsTracker = (*Connector)(nil)
var _ sdk.LogisticsLabelReader = (*Connector)(nil)
