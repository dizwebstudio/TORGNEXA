// Package ozonpay adapts the Ozon Pay account boundary to the provider-neutral
// Connector SDK. Payment mutations stay disabled until the merchant API
// contract is qualified; the admitted surface currently verifies Seller API
// access only.
package ozonpay

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// Connector exposes the separate Ozon Pay finance-surface health check.
type Connector struct {
	transport Transport
	now       func() time.Time
}

// New constructs an Ozon Pay connector over a host-mediated transport.
func New(transport Transport, now func() time.Time) *Connector {
	if now == nil {
		now = time.Now
	}
	return &Connector{transport: transport, now: now}
}

// Manifest returns the canonical Ozon Pay connector manifest.
func Manifest() sdk.Manifest { return manifest() }

// Manifest returns the canonical connector manifest through the SDK root.
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

// Health verifies the tenant-scoped Ozon Seller API credentials. A healthy
// result confirms API access, not merchant activation for Ozon Pay.
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
	value, _ := sdk.CatalogManifest("ozon-pay")
	return value
}

var _ sdk.Connector = (*Connector)(nil)
