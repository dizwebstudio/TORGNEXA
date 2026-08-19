package cbrfx

import (
	"context"
	"errors"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var ErrTransportMissing = errors.New("cbr-fx: transport missing")

type Transport interface {
	Daily(context.Context, time.Time) ([]byte, error)
}

type Connector struct {
	transport Transport
	now       func() time.Time
}

func New(transport Transport, now func() time.Time) *Connector {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Connector{transport: transport, now: now}
}
func Manifest() sdk.Manifest                { manifest, _ := sdk.CatalogManifest("cbr-fx"); return manifest }
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }
func (c *Connector) Health(ctx context.Context, account sdk.Account, _ sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	at := c.now().UTC()
	body, err := c.transport.Daily(ctx, at)
	if err != nil {
		return sdk.Health{Status: sdk.HealthUnavailable, ReasonCode: "remote_unavailable", CheckedAt: at}, nil
	}
	if _, err = parseDaily(body, at); err != nil {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "remote_contract_invalid", CheckedAt: at}, nil
	}
	return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: at}, nil
}
func remote(cat sdk.ErrorCategory, code string, retry time.Duration) error {
	e, _ := sdk.NewRemoteError(cat, code, "", retry)
	return e
}
