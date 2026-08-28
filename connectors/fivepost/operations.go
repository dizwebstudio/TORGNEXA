package fivepost

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// Transport is the host-mediated boundary for the 5Post partner API.
// Implementations must keep API keys/JWTs inside the callback scope and must
// not expose raw provider payloads to Core.
type Transport interface {
	Ping(context.Context, []byte) error
	Create(context.Context, []byte, sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error)
	Track(context.Context, []byte, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error)
	Cancel(context.Context, []byte, sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error)
	Label(context.Context, []byte, sdk.LabelRequest) (sdk.LabelResult, error)
	Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error)
}

func manifest() sdk.Manifest {
	manifest, _ := sdk.CatalogManifest("fivepost")
	return manifest
}

// CreateLogisticsShipment creates a partner shipment and normalizes its identity.
func (c *Connector) CreateLogisticsShipment(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	if request.Validate() != nil {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var result sdk.ShipmentResult
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		result, callErr = c.transport.Create(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	return validateShipment(result)
}

// ReadLogisticsTracking reads the current remote shipment status.
func (c *Connector) ReadLogisticsTracking(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	if request.RemoteID == "" {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var result sdk.ShipmentResult
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		result, callErr = c.transport.Track(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	return validateShipment(result)
}

// CancelLogisticsShipment cancels a remote shipment with an idempotency key.
func (c *Connector) CancelLogisticsShipment(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	if request.RemoteID == "" || request.IdempotencyKey == "" {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
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
	return validateShipment(result)
}

// ReadLogisticsLabel returns a host-stored label artifact reference.
func (c *Connector) ReadLogisticsLabel(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.LabelRequest) (sdk.LabelResult, error) {
	if request.RemoteID == "" || request.Format == "" {
		return sdk.LabelResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var result sdk.LabelResult
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		result, callErr = c.transport.Label(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.LabelResult{}, err
	}
	if result.ArtifactRef == "" || result.MediaType == "" || result.ObservedAt.IsZero() {
		return sdk.LabelResult{}, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
	}
	return result, nil
}

// ReadPickupPoints reads the bounded 5Post pickup-point directory.
func (c *Connector) ReadPickupPoints(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	if query.Validate(500) != nil {
		return nil, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	var result []sdk.PickupPoint
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		result, callErr = c.transport.Pickup(ctx, secret, query)
		return callErr
	})
	if err != nil {
		return nil, err
	}
	for _, point := range result {
		if point.RemoteID == "" || point.Name == "" || point.Country == "" || point.City == "" || point.Address == "" || point.UpdatedAt.IsZero() {
			return nil, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
		}
	}
	return result, nil
}

func validateShipment(result sdk.ShipmentResult) (sdk.ShipmentResult, error) {
	if result.RemoteID == "" || result.Status == "" || result.Cost.Validate() != nil || result.ObservedAt.IsZero() {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
	}
	return result, nil
}

var _ sdk.LogisticsShipmentCreator = (*Connector)(nil)
var _ sdk.LogisticsTracker = (*Connector)(nil)
var _ sdk.LogisticsShipmentCanceler = (*Connector)(nil)
var _ sdk.LogisticsLabelReader = (*Connector)(nil)
var _ sdk.PickupPointReader = (*Connector)(nil)
