package lamoda

import "context"

// Transport is the host-mediated boundary for the Lamoda Seller API.
// Plaintext credentials are valid only for the duration of Ping.
type Transport interface {
	Ping(context.Context, []byte) error
}
