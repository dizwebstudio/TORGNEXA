package ozondelivery

import "context"

// Transport is the host-mediated Ozon Seller API boundary. Secret bytes are
// valid only while the Health callback is running.
type Transport interface {
	Ping(context.Context, []byte) error
}
