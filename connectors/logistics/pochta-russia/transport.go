package pochtarussia

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// Transport is the host-mediated Russian Post Otpravka API boundary.
// Plaintext credentials are valid only for the duration of each callback.
type Transport interface {
	Ping(context.Context, []byte) error
	Rates(context.Context, []byte, sdk.RateRequest) ([]sdk.RateQuote, error)
	Track(context.Context, []byte, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error)
	Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error)
}
