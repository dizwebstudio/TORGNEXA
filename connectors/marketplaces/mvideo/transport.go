package mvideo

import "context"

// Transport is the host-mediated boundary for the М.Видео partner API.
// Plaintext credentials are valid only for the duration of Ping.
type Transport interface {
	Ping(context.Context, []byte) error
}
