// Package ozondelivery adapts the Ozon Delivery account boundary to the
// provider-neutral Connector SDK. Shipment creation and tracking remain
// qualification-gated; the admitted surface verifies Seller API access.
package ozondelivery

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// Connector exposes the separate Ozon Delivery logistics-surface health check.
type Connector struct {
	transport Transport
	now       func() time.Time
}

// New constructs an Ozon Delivery connector over a host-mediated transport.
func New(transport Transport, now func() time.Time) *Connector {
	if now == nil {
		now = time.Now
	}
	return &Connector{transport: transport, now: now}
}

// Manifest returns the canonical Ozon Delivery connector manifest.
func Manifest() sdk.Manifest { return manifest() }

// Manifest returns the canonical connector manifest through the SDK root.
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

// Health verifies the tenant-scoped Seller API credentials. A healthy result
// confirms API access, not eligibility or activation of every Ozon Delivery
// service for the merchant.
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
	value, _ := sdk.CatalogManifest("ozon-delivery")
	return value
}

var _ sdk.Connector = (*Connector)(nil)
