package dellin

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// Transport is the host-mediated boundary for the Деловые Линии API.
// Plaintext credentials are valid only for the duration of Ping.
type Transport interface {
	Ping(context.Context, []byte) error
	Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error)
}
