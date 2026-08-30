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
	Track(context.Context, []byte, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error)
	Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error)
}
