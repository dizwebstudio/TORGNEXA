package sabyedo

import (
	"context"
	"errors"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"time"
)

var ErrTransport = errors.New("saby-edo: transport unavailable")

type Connector struct {
	transport Transport
	now       func() time.Time
}

func New(t Transport, now func() time.Time) *Connector {
	if now == nil {
		now = time.Now
	}
	return &Connector{transport: t, now: now}
}
func Manifest() sdk.Manifest                { return manifest() }
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }
func (c *Connector) Health(ctx context.Context, a sdk.Account, r sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil {
		return sdk.Health{}, remote(sdk.ErrorInvalidRequest, "health_rejected", 0)
	}
	var pingErr error
	err := r.Secrets().UseSecret(ctx, a.SecretReference, func(secret []byte) error {
		if len(secret) == 0 {
			return remote(sdk.ErrorUnauthorized, "credential_missing", 0)
		}
		pingErr = c.transport.Ping(ctx, secret)
		return nil
	})
	if err != nil {
		return sdk.Health{}, err
	}
	if pingErr != nil {
		return sdk.Health{Status: sdk.HealthUnavailable, ReasonCode: "provider_unavailable", CheckedAt: c.now().UTC()}, nil
	}
	return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: c.now().UTC()}, nil
}
func remote(cat sdk.ErrorCategory, code string, retry time.Duration) error {
	e, _ := sdk.NewRemoteError(cat, code, "", retry)
	return e
}
func useSecret(ctx context.Context, r sdk.Runtime, a sdk.Account, fn func([]byte) error) error {
	if r == nil || r.Secrets() == nil {
		return remote(sdk.ErrorUnauthorized, "credential_missing", 0)
	}
	return r.Secrets().UseSecret(ctx, a.SecretReference, fn)
}
