// Package mvideo contains the SDK boundary for the М.Видео partner API.
// Domain operations remain qualification-gated; the built-in runtime currently
// composes the generic, operator-configured health probe only.
package mvideo

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// Connector exposes the provider-neutral М.Видео health-check contract.
type Connector struct {
	transport Transport
	now       func() time.Time
}

// New constructs a М.Видео connector over a host-mediated transport.
func New(transport Transport, now func() time.Time) *Connector {
	if now == nil {
		now = time.Now
	}
	return &Connector{transport: transport, now: now}
}

// Manifest returns the canonical М.Видео connector manifest.
func Manifest() sdk.Manifest { value, _ := sdk.CatalogManifest("mvideo"); return value }

// Manifest returns the connector manifest through the SDK root contract.
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

// Health validates tenant-scoped credentials without persisting plaintext.
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
