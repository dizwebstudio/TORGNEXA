package dellin

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// Connector exposes the reviewed Delivery-surface health check for Деловые
// Линии. Shipment operations remain behind a future qualified transport.
type Connector struct {
	transport Transport
	now       func() time.Time
}

// New constructs a Деловые Линии connector over a host-mediated transport.
func New(transport Transport, now func() time.Time) *Connector {
	if now == nil {
		now = time.Now
	}
	return &Connector{transport: transport, now: now}
}

// Manifest returns the canonical non-secret connector manifest.
func Manifest() sdk.Manifest { return manifest() }

// Manifest returns the canonical non-secret connector manifest.
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

// Health validates the tenant-scoped API credentials without persisting the
// session identifier returned by the provider.
func (c *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, remote(sdk.ErrorInvalidRequest, "health_rejected")
	}
	var probeErr error
	err := runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		if len(secret) == 0 {
			probeErr = remote(sdk.ErrorUnauthorized, "credential_missing")
			return nil
		}
		probeErr = c.transport.Ping(ctx, secret)
		return nil
	})
	if err != nil {
		return sdk.Health{}, err
	}
	if probeErr != nil {
		return sdk.Health{Status: sdk.HealthUnavailable, ReasonCode: "provider_unavailable", CheckedAt: c.now().UTC()}, nil
	}
	return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: c.now().UTC()}, nil
}

func remote(category sdk.ErrorCategory, code string) error {
	err, _ := sdk.NewRemoteError(category, code, "", 0)
	return err
}

func manifest() sdk.Manifest {
	value, _ := sdk.CatalogManifest("dellin")
	return value
}

var _ sdk.Connector = (*Connector)(nil)
