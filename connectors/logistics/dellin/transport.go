package dellin

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// Transport is the host-mediated boundary for the Деловые Линии API.
// Plaintext credentials are valid only for the duration of Ping.
type Transport interface {
	Ping(context.Context, []byte) error
	Rates(context.Context, []byte, sdk.RateRequest) ([]sdk.RateQuote, error)
	Create(context.Context, []byte, sdk.ShipmentCreateRequest, Configuration) (sdk.ShipmentResult, error)
	Cancel(context.Context, []byte, sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error)
	CancelBatch(context.Context, []byte, sdk.LogisticsBatchCancelRequest) (sdk.LogisticsBatchCancellation, error)
	Track(context.Context, []byte, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error)
	Label(context.Context, []byte, sdk.LabelRequest) (sdk.LabelResult, error)
	Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error)
}
