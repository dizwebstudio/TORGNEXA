package fivepost

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// Connector adapts the partner 5Post API to the provider-neutral logistics SDK.
// Network access is supplied by the host through Transport.
type Connector struct {
	transport Transport
	now       func() time.Time
}

// New constructs a 5Post connector with a host-owned transport.
func New(transport Transport, now func() time.Time) *Connector {
	if now == nil {
		now = time.Now
	}
	return &Connector{transport: transport, now: now}
}

// Manifest returns the canonical 5Post connector manifest.
func Manifest() sdk.Manifest { return manifest() }

// Manifest returns the canonical 5Post connector manifest.
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

// Health checks the partner credential through the host transport.
func (c *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, remote(sdk.ErrorInvalidRequest, "health_rejected", 0)
	}
	var pingErr error
	err := runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
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

func remote(category sdk.ErrorCategory, code string, retry time.Duration) error {
	err, _ := sdk.NewRemoteError(category, code, "", retry)
	return err
}

func useSecret(ctx context.Context, runtime sdk.Runtime, account sdk.Account, fn func([]byte) error) error {
	if runtime == nil || runtime.Secrets() == nil {
		return remote(sdk.ErrorUnauthorized, "credential_missing", 0)
	}
	return runtime.Secrets().UseSecret(ctx, account.SecretReference, fn)
}
