// Package pochtarussia adapts the Russian Post "Otpravka" account boundary
// to the provider-neutral Connector SDK. Network access is supplied by the
// host through Transport.
package pochtarussia

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// Connector exposes the separate Delivery-surface health check for Russian
// Post. Shipment operations remain qualification-gated until the current
// Otpravka contract is exercised with a non-production account.
type Connector struct {
	transport Transport
	now       func() time.Time
}

// New constructs a Russian Post connector over a host-mediated transport.
func New(transport Transport, now func() time.Time) *Connector {
	if now == nil {
		now = time.Now
	}
	return &Connector{transport: transport, now: now}
}

// Manifest returns the canonical non-secret connector manifest.
func Manifest() sdk.Manifest { return manifest() }

func manifest() sdk.Manifest {
	m, _ := sdk.CatalogManifest("pochta-russia")
	return m
}

// Manifest returns the canonical connector manifest through the SDK root.
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

// Health validates the tenant-scoped Otpravka credentials without persisting
// either the application token or the generated user authorization key.
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

var _ sdk.Connector = (*Connector)(nil)
