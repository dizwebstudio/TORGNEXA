// Package dolyami adapts the Долями merchant API boundary to the provider-
// neutral Connector SDK. The current built-in admission is health-only: the
// payment operation bridge remains qualification-gated.
package dolyami

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// Connector exposes the separate finance-surface health check.
type Connector struct {
	transport Transport
	configs   ConfigurationSource
	now       func() time.Time
}

// New constructs a Долями connector over host-mediated transport and config.
func New(transport Transport, configs ConfigurationSource, now func() time.Time) *Connector {
	if now == nil {
		now = time.Now
	}
	return &Connector{transport: transport, configs: configs, now: now}
}

// Manifest returns the canonical Долями connector manifest.
func Manifest() sdk.Manifest { value, _ := sdk.CatalogManifest("dolyami"); return value }

// Manifest returns the connector manifest through the SDK root contract.
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

func (c *Connector) configuration(ctx context.Context, account sdk.Account) (Configuration, error) {
	if c == nil || c.configs == nil {
		return Configuration{}, ErrConfigurationMissing
	}
	configuration, err := c.configs.Resolve(ctx, account)
	if err != nil {
		return Configuration{}, ErrConfigurationMissing
	}
	if err := configuration.Validate(); err != nil {
		return Configuration{}, err
	}
	return configuration, nil
}

// Health verifies the mTLS/basic credential bundle against the configured
// merchant endpoint. A healthy result does not enable payment mutations.
func (c *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, remote(sdk.ErrorInvalidRequest, "health_rejected")
	}
	configuration, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "configuration_invalid", CheckedAt: c.now().UTC()}, nil
	}
	var pingErr error
	err = runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		if len(secret) == 0 {
			pingErr = remote(sdk.ErrorUnauthorized, "credential_missing")
			return nil
		}
		pingErr = c.transport.Ping(ctx, configuration, secret)
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

func remote(category sdk.ErrorCategory, code string) error {
	err, _ := sdk.NewRemoteError(category, code, "", 0)
	return err
}

var _ sdk.Connector = (*Connector)(nil)
