package pek

import (
	"context"
	"regexp"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var pekCargoCodePattern = regexp.MustCompile(`^[0-9]{1,18}$`)
var pekIdempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

// Transport is the host-mediated boundary for the ПЭК API. Plaintext
// credentials are valid only for the duration of each callback invocation.
type Transport interface {
	Ping(context.Context, []byte) error
	Rates(context.Context, []byte, sdk.RateRequest) ([]sdk.RateQuote, error)
	Create(context.Context, []byte, sdk.ShipmentCreateRequest, Configuration) (sdk.ShipmentResult, error)
	Cancel(context.Context, []byte, sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error)
	Track(context.Context, []byte, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error)
	Label(context.Context, []byte, sdk.LabelRequest) (sdk.LabelResult, error)
	Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error)
}

func manifest() sdk.Manifest {
	manifest, _ := sdk.CatalogManifest("pek")
	return manifest
}

// ReadLogisticsRates calculates delivery rates for the requested route.
func (c *Connector) ReadLogisticsRates(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.RateRequest) ([]sdk.RateQuote, error) {
	if request.Validate() != nil {
		return nil, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
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
			return nil, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
		}
	}
	return result, nil
}

// CreateLogisticsShipment creates a pre-registration request in ПЭК.
func (c *Connector) CreateLogisticsShipment(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	if c.configuration == nil {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "configuration_rejected", 0)
	}
	configuration, err := c.configuration.Resolve(ctx, account)
	if err != nil {
		return sdk.ShipmentResult{}, err
	}
	if configuration.Validate() != nil {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "configuration_rejected", 0)
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
	return validateShipment(result)
}

// CancelLogisticsShipment annuls one previously created ПЭК pre-registration.
// The provider endpoint is intentionally limited to one cargo code so a
// caller cannot accidentally turn this neutral operation into a batch write.
func (c *Connector) CancelLogisticsShipment(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	remoteID := strings.TrimSpace(request.RemoteID)
	if c == nil || c.transport == nil || !pekCargoCodePattern.MatchString(remoteID) || !pekIdempotencyKeyPattern.MatchString(request.IdempotencyKey) {
		return sdk.ShipmentResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	request.RemoteID = remoteID
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

// ReadLogisticsLabel requests one PDF label for one ПЭК cargo code. The
// provider also supports batch and application forms, but those are not
// exposed through this single-label neutral contract.
func (c *Connector) ReadLogisticsLabel(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.LabelRequest) (sdk.LabelResult, error) {
	remoteID := strings.TrimSpace(request.RemoteID)
	if c == nil || c.transport == nil || !pekCargoCodePattern.MatchString(remoteID) || strings.ToLower(strings.TrimSpace(request.Format)) != "pdf" {
		return sdk.LabelResult{}, remote(sdk.ErrorInvalidRequest, "request_rejected", 0)
	}
	request.RemoteID = remoteID
	request.Format = "pdf"
	var result sdk.LabelResult
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		result, callErr = c.transport.Label(ctx, secret, request)
		return callErr
	})
	if err != nil {
		return sdk.LabelResult{}, err
	}
	if result.ArtifactRef == "" || result.MediaType != "application/pdf" || result.ObservedAt.IsZero() {
		return sdk.LabelResult{}, remote(sdk.ErrorInternal, "invalid_remote_response", 0)
	}
	return result, nil
}

// ReadLogisticsTracking reads the current cargo status.
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

// ReadPickupPoints resolves ПЭК branches for a bounded location query.
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
		if point.Validate() != nil {
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

var _ sdk.LogisticsRateReader = (*Connector)(nil)
var _ sdk.LogisticsShipmentCreator = (*Connector)(nil)
var _ sdk.LogisticsShipmentCanceler = (*Connector)(nil)
var _ sdk.LogisticsTracker = (*Connector)(nil)
var _ sdk.LogisticsLabelReader = (*Connector)(nil)
var _ sdk.PickupPointReader = (*Connector)(nil)
