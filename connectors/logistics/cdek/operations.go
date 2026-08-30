package cdek

import (
	"context"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type Transport interface {
	Ping(context.Context, []byte) error
	Rates(context.Context, []byte, sdk.RateRequest) ([]sdk.RateQuote, error)
	Create(context.Context, []byte, sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error)
	Track(context.Context, []byte, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error)
	Cancel(context.Context, []byte, sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error)
	Return(context.Context, []byte, sdk.ReturnCreateRequest) (sdk.ShipmentResult, error)
	Label(context.Context, []byte, sdk.LabelRequest) (sdk.LabelResult, error)
	Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error)
	Webhook(context.Context, []byte, []byte, []byte) (sdk.LogisticsWebhook, error)
}

func manifest() sdk.Manifest { manifest, _ := sdk.CatalogManifest("cdek"); return manifest }
func (c *Connector) ReadLogisticsRates(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.RateRequest) ([]sdk.RateQuote, error) {
	if q.Validate() != nil {
		return nil, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out []sdk.RateQuote
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.Rates(ctx, s, q); return x })
	if e != nil {
		return nil, e
	}
	for _, v := range out {
		if v.Validate() != nil {
			return nil, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
		}
	}
	return out, nil
}
func (c *Connector) CreateLogisticsShipment(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	if q.Validate() != nil {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out sdk.ShipmentResult
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.Create(ctx, s, q); return x })
	if e != nil {
		return sdk.ShipmentResult{}, e
	}
	return validateShipment(out)
}
func (c *Connector) ReadLogisticsTracking(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	if q.RemoteID == "" {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out sdk.ShipmentResult
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.Track(ctx, s, q); return x })
	if e != nil {
		return sdk.ShipmentResult{}, e
	}
	return validateShipment(out)
}
func (c *Connector) CancelLogisticsShipment(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	if q.RemoteID == "" || q.IdempotencyKey == "" {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out sdk.ShipmentResult
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.Cancel(ctx, s, q); return x })
	if e != nil {
		return sdk.ShipmentResult{}, e
	}
	return validateShipment(out)
}
func (c *Connector) CreateLogisticsReturn(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.ReturnCreateRequest) (sdk.ShipmentResult, error) {
	if q.OriginalRemoteID == "" || q.ExternalID == "" || q.IdempotencyKey == "" {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out sdk.ShipmentResult
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.Return(ctx, s, q); return x })
	if e != nil {
		return sdk.ShipmentResult{}, e
	}
	return validateShipment(out)
}
func (c *Connector) ReadLogisticsLabel(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.LabelRequest) (sdk.LabelResult, error) {
	if q.Validate() != nil {
		return sdk.LabelResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out sdk.LabelResult
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.Label(ctx, s, q); return x })
	if e != nil {
		return sdk.LabelResult{}, e
	}
	if out.ArtifactRef == "" || out.MediaType == "" || out.ObservedAt.IsZero() {
		return sdk.LabelResult{}, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
	}
	return out, nil
}
func (c *Connector) ReadPickupPoints(ctx context.Context, a sdk.Account, r sdk.Runtime, q sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	if q.Validate(500) != nil {
		return nil, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var out []sdk.PickupPoint
	e := useSecret(ctx, r, a, func(s []byte) error { var x error; out, x = c.transport.Pickup(ctx, s, q); return x })
	if e != nil {
		return nil, e
	}
	for _, p := range out {
		if p.Validate() != nil {
			return nil, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
		}
	}
	return out, nil
}
func validateShipment(out sdk.ShipmentResult) (sdk.ShipmentResult, error) {
	if out.RemoteID == "" || out.Status == "" || out.Cost.Validate() != nil || out.ObservedAt.IsZero() {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
	}
	return out, nil
}

var _ sdk.LogisticsRateReader = (*Connector)(nil)
var _ sdk.LogisticsShipmentCreator = (*Connector)(nil)
var _ sdk.LogisticsTracker = (*Connector)(nil)
var _ sdk.LogisticsShipmentCanceler = (*Connector)(nil)
var _ sdk.LogisticsReturnCreator = (*Connector)(nil)
var _ sdk.LogisticsLabelReader = (*Connector)(nil)
var _ sdk.PickupPointReader = (*Connector)(nil)

func (c *Connector) VerifyLogisticsWebhook(ctx context.Context, a sdk.Account, r sdk.Runtime, body, signature []byte) (sdk.LogisticsWebhook, error) {
	if len(body) == 0 || len(body) > 2<<20 || len(signature) == 0 {
		return sdk.LogisticsWebhook{}, remote(sdk.ErrorInvalidRequest, "webhook_rejected", 0)
	}
	var out sdk.LogisticsWebhook
	err := useSecret(ctx, r, a, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.Webhook(ctx, secret, body, signature)
		return callErr
	})
	if err != nil {
		return sdk.LogisticsWebhook{}, err
	}
	if out.Validate() != nil {
		return sdk.LogisticsWebhook{}, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
	}
	return out, nil
}

var _ sdk.LogisticsWebhookVerifier = (*Connector)(nil)
