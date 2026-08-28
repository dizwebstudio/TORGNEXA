package ozonpay

import "context"

// candidateTransport is deterministic and never performs network I/O.
type candidateTransport struct{}

func (candidateTransport) Ping(context.Context, []byte) error { return nil }
