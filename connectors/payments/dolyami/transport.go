package dolyami

import "context"

// Transport is the host-mediated Долями API boundary. Secret material is
// valid only while Ping is running and must never be retained by a connector.
type Transport interface {
	Ping(context.Context, Configuration, []byte) error
}
