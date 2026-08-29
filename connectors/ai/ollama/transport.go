package ollama

import "context"

// local transport is intentionally a narrow alias boundary implemented by the
// reviewed built-in runtime package.
type hostTransport interface {
	Do(context.Context, Request) (Response, error)
}
