package openwebui

import "context"

type hostTransport interface {
	Do(context.Context, Request) (Response, error)
}
