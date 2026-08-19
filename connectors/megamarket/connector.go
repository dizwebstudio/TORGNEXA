package megamarket

import (
	"bytes"
	"context"
	"errors"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	apiHost      = "api.megamarket.tech"
	maxBodyBytes = 12 << 20
)

var (
	ErrTransportMissing   = errors.New("megamarket: transport missing")
	ErrInvalidResponse    = errors.New("megamarket: invalid response")
	ErrInvalidCredentials = errors.New("megamarket: invalid credentials")
)

type Request struct {
	Method, Host, Path string
	Body               []byte
	MerchantToken      []byte
}
type Response struct {
	StatusCode   int
	Body         []byte
	RequestID    string
	RetryAfterMS int64
}
type Transport interface {
	Do(context.Context, Request) (Response, error)
}
type Connector struct {
	transport Transport
	configs   ConfigurationSource
	now       func() time.Time
}

func New(t Transport, c ConfigurationSource, now func() time.Time) *Connector {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Connector{t, c, now}
}
func Manifest() sdk.Manifest                { manifest, _ := sdk.CatalogManifest("megamarket"); return manifest }
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }
func (c *Connector) configuration(ctx context.Context, a sdk.Account) (Configuration, error) {
	if c == nil || c.configs == nil {
		return Configuration{}, ErrConfigurationMissing
	}
	v, e := c.configs.Resolve(ctx, a)
	if e != nil {
		return Configuration{}, ErrConfigurationMissing
	}
	if e = v.Validate(); e != nil {
		return Configuration{}, e
	}
	return v, nil
}
func (c *Connector) withToken(ctx context.Context, r sdk.Runtime, ref sdk.SecretReference, cb func([]byte) error) error {
	if c == nil || r == nil || r.Secrets() == nil || cb == nil {
		return ErrInvalidCredentials
	}
	return r.Secrets().UseSecret(ctx, ref, func(s []byte) error {
		if !validToken(s) {
			return ErrInvalidCredentials
		}
		return cb(s)
	})
}
func validToken(v []byte) bool {
	if len(v) < 16 || len(v) > 4096 || !utf8.Valid(v) || !bytes.Equal(v, bytes.TrimSpace(v)) {
		return false
	}
	for _, b := range v {
		if b <= 0x20 || b == 0x7f {
			return false
		}
	}
	return true
}
func (c *Connector) Health(ctx context.Context, a sdk.Account, r sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	at := c.now().UTC()
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "configuration_invalid", CheckedAt: at}, nil
	}
	body := []byte(`{"filter":{},"sorting":{"field":"offerId","order":"asc"},"limit":1}`)
	e = c.withToken(ctx, r, a.SecretReference, func(k []byte) error {
		resp, ce := c.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/api/merchantIntegration/assortment/v1/card/getAttributes", Body: body, MerchantToken: k})
		if ce != nil {
			return normalizedTransportError()
		}
		if re := normalizeHTTP(resp); re != nil {
			return re
		}
		_, pe := parseProducts(resp.Body, 1, cfg, "")
		return pe
	})
	if e == nil {
		return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: at}, nil
	}
	var re *sdk.RemoteError
	if errors.As(e, &re) {
		st, rc := sdk.HealthUnavailable, "remote_unavailable"
		if re.Category == sdk.ErrorUnauthorized || re.Category == sdk.ErrorForbidden {
			st, rc = sdk.HealthDegraded, "auth_rejected"
		} else if re.Category == sdk.ErrorRateLimited {
			st, rc = sdk.HealthDegraded, "rate_limited"
		}
		return sdk.Health{Status: st, ReasonCode: rc, CheckedAt: at}, nil
	}
	return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "remote_contract_invalid", CheckedAt: at}, nil
}
func normalizedTransportError() error {
	r, _ := sdk.NewRemoteError(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
	return r
}
func normalizeHTTP(r Response) error {
	if len(r.Body) > maxBodyBytes || r.RetryAfterMS < 0 {
		x, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return x
	}
	if r.StatusCode >= 200 && r.StatusCode < 300 {
		return nil
	}
	cat, code := sdk.ErrorTransient, "remote_error"
	switch r.StatusCode {
	case 400, 405, 406, 415, 422:
		cat, code = sdk.ErrorInvalidRequest, "request_rejected"
	case 401:
		cat, code = sdk.ErrorUnauthorized, "auth_rejected"
	case 403:
		cat, code = sdk.ErrorForbidden, "access_denied"
	case 404:
		cat, code = sdk.ErrorNotFound, "resource_missing"
	case 409, 423:
		cat, code = sdk.ErrorConflict, "remote_conflict"
	case 429:
		cat, code = sdk.ErrorRateLimited, "rate_limited"
	case 500, 502, 503, 504:
		cat, code = sdk.ErrorUnavailable, "remote_unavailable"
	}
	retry := time.Duration(0)
	if cat == sdk.ErrorRateLimited || cat == sdk.ErrorUnavailable || cat == sdk.ErrorTransient {
		retry = time.Duration(r.RetryAfterMS) * time.Millisecond
	}
	x, e := sdk.NewRemoteError(cat, code, r.RequestID, retry)
	if e != nil {
		x, _ = sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
	}
	return x
}
