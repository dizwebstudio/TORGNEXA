package dellin

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// Connector exposes the reviewed Delivery-surface operations for Деловые
// Линии. Shipment creation uses an explicit tenant configuration.
type Connector struct {
	transport Transport
	configs   ConfigurationSource
	now       func() time.Time
}

// New constructs a Деловые Линии connector over a host-mediated transport.
func New(transport Transport, now func() time.Time, configs ...ConfigurationSource) *Connector {
	if now == nil {
		now = time.Now
	}
	var source ConfigurationSource
	if len(configs) > 0 {
		source = configs[0]
	}
	return &Connector{transport: transport, configs: source, now: now}
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

func (c *Connector) configuration(ctx context.Context, account sdk.Account) (Configuration, error) {
	if c == nil || c.configs == nil {
		return Configuration{}, ErrConfigurationMissing
	}
	configuration, err := c.configs.Resolve(ctx, account)
	if err != nil {
		return Configuration{}, err
	}
	if configuration.Validate() != nil {
		return Configuration{}, ErrInvalidConfiguration
	}
	return configuration, nil
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
