package dellin

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// ReadPickupPoints reads a bounded Деловые Линии terminal/PUDO directory.
// Provider identifiers stay remote references and never become Core warehouse
// identifiers.
func (c *Connector) ReadPickupPoints(ctx context.Context, account sdk.Account, runtime sdk.Runtime, query sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || query.Validate(500) != nil {
		return nil, remote(sdk.ErrorInvalidRequest, "request_rejected")
	}
	var out []sdk.PickupPoint
	err := useSecret(ctx, runtime, account, func(secret []byte) error {
		var callErr error
		out, callErr = c.transport.Pickup(ctx, secret, query)
		return callErr
	})
	if err != nil {
		return nil, err
	}
	for _, point := range out {
		if point.Validate() != nil {
			return nil, remote(sdk.ErrorInternal, "invalid_remote_response")
		}
	}
	return out, nil
}

func useSecret(ctx context.Context, runtime sdk.Runtime, account sdk.Account, fn func([]byte) error) error {
	if runtime == nil || runtime.Secrets() == nil {
		return remote(sdk.ErrorUnauthorized, "credential_missing")
	}
	return runtime.Secrets().UseSecret(ctx, account.SecretReference, fn)
}

var _ sdk.PickupPointReader = (*Connector)(nil)
