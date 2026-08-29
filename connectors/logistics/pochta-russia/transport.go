package pochtarussia

import "context"

// Transport is the host-mediated Russian Post Otpravka API boundary.
// Plaintext credentials are valid only for the duration of Ping.
type Transport interface {
	Ping(context.Context, []byte) error
}
