package pochtarussia

import "context"

// candidateTransport is deterministic and never opens a network connection.
// It is used by unit and conformance tests only.
type candidateTransport struct{}

func (candidateTransport) Ping(context.Context, []byte) error { return nil }
