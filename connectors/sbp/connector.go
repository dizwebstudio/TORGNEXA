package sbp

import (
	"context"
	"errors"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"time"
)

var ErrTransport = errors.New("sbp: transport unavailable")

type Connector struct {
	transport Transport
	configs   ConfigurationSource
	now       func() time.Time
}

func New(t Transport, configs ConfigurationSource, now func() time.Time) *Connector {
	if now == nil {
		now = time.Now
	}
	return &Connector{transport: t, configs: configs, now: now}
}
func Manifest() sdk.Manifest                { return manifest() }
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
func (c *Connector) Health(ctx context.Context, a sdk.Account, r sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil {
		return sdk.Health{}, remote(sdk.ErrorInvalidRequest, "health_rejected", 0)
	}
	configuration, err := c.configuration(ctx, a)
	if err != nil {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "configuration_invalid", CheckedAt: c.now().UTC()}, nil
	}
	var pingErr error
	err = r.Secrets().UseSecret(ctx, a.SecretReference, func(secret []byte) error {
		if len(secret) == 0 {
			return remote(sdk.ErrorUnauthorized, "credential_missing", 0)
		}
		pingErr = c.transport.Ping(ctx, configuration.GatewayHost, secret)
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
