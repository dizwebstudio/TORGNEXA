package opencart

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const maxBodyBytes = 16 << 20

var (
	ErrTransportMissing   = errors.New("opencart: transport missing")
	ErrInvalidResponse    = errors.New("opencart: invalid response")
	ErrInvalidCredentials = errors.New("opencart: invalid credentials")
)

type QueryParam struct{ Name, Value string }
type Request struct {
	Method, Host, Path string
	Query              []QueryParam
	Body               []byte
	Token              []byte
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

func New(transport Transport, configs ConfigurationSource, now func() time.Time) *Connector {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Connector{transport: transport, configs: configs, now: now}
}
func Manifest() sdk.Manifest {
	return sdk.Manifest{ID: "opencart", Name: "OpenCart", Family: sdk.FamilyMarketplace, Version: "1.0.0", SDKVersion: sdk.SDKMajor, Capabilities: []sdk.Capability{"inventory.read", "inventory.write", "orders.read", "orders.status.write", "prices.read", "prices.write", "products.read", "products.write"}, Auth: []sdk.AuthRequirement{{Kind: sdk.AuthBearer, SecretClass: "storefront.bridge-token", Required: true}}, RateLimit: sdk.RateLimitPolicy{MaxConcurrency: 4, MinIntervalMS: 100, RequestTimeoutMS: 30000, Retry: sdk.RetryPolicy{MaxAttempts: 5, BaseBackoffMS: 500, MaxBackoffMS: 30000}}}
}
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }
func (c *Connector) configuration(ctx context.Context, a sdk.Account) (Configuration, error) {
	if c == nil || c.configs == nil {
		return Configuration{}, ErrConfigurationMissing
	}
	cfg, err := c.configs.Resolve(ctx, a)
	if err != nil || cfg.Validate() != nil {
		return Configuration{}, ErrInvalidConfiguration
	}
	return cfg, nil
}

type credentials struct {
	Token string `json:"token"`
}

func parseCredentials(raw []byte) (credentials, error) {
	if len(raw) < 24 || len(raw) > 8192 || !utf8.Valid(raw) || !bytes.Equal(raw, bytes.TrimSpace(raw)) {
		return credentials{}, ErrInvalidCredentials
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var v credentials
	if dec.Decode(&v) != nil {
		return credentials{}, ErrInvalidCredentials
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return credentials{}, ErrInvalidCredentials
	}
	if len(v.Token) < 24 || len(v.Token) > 4096 || v.Token != strings.TrimSpace(v.Token) {
		return credentials{}, ErrInvalidCredentials
	}
	for _, r := range v.Token {
		if r <= 0x20 || r == 0x7f {
			return credentials{}, ErrInvalidCredentials
		}
	}
	return v, nil
}
func (c *Connector) withCredentials(ctx context.Context, r sdk.Runtime, ref sdk.SecretReference, cb func(credentials) error) error {
	if c == nil || r == nil || r.Secrets() == nil || cb == nil {
		return ErrInvalidCredentials
	}
	return r.Secrets().UseSecret(ctx, ref, func(secret []byte) error {
		v, e := parseCredentials(secret)
		if e != nil {
			return e
		}
		defer func() { v.Token = "" }()
		return cb(v)
	})
}
func routeQuery(route string, q []QueryParam) []QueryParam {
	out := make([]QueryParam, 0, len(q)+1)
	out = append(out, QueryParam{Name: "route", Value: "extension/torgnexa/api/" + route})
	return append(out, q...)
}
func (c *Connector) call(ctx context.Context, cfg Configuration, cred credentials, method, route string, q []QueryParam, body []byte) (Response, error) {
	if c == nil || c.transport == nil {
		return Response{}, ErrTransportMissing
	}
	token := []byte(cred.Token)
	defer clear(token)
	resp, err := c.transport.Do(ctx, Request{Method: method, Host: cfg.StoreHost, Path: cfg.apiPath(), Query: routeQuery(route, q), Body: body, Token: token})
	if err != nil {
		return Response{}, normalizedTransportError()
	}
	if remote := normalizeHTTP(resp); remote != nil {
		return resp, remote
	}
	return resp, nil
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
	e = c.withCredentials(ctx, r, a.SecretReference, func(cred credentials) error {
		resp, err := c.call(ctx, cfg, cred, "GET", "health", nil, nil)
		if err != nil {
			return err
		}
		var v struct {
			OK         bool   `json:"ok"`
			APIVersion string `json:"api_version"`
		}
		if json.Unmarshal(resp.Body, &v) != nil || !v.OK || v.APIVersion != "v1" {
			return ErrInvalidResponse
		}
		return nil
	})
	if e == nil {
		return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: at}, nil
	}
	var remote *sdk.RemoteError
	if errors.As(e, &remote) {
		status, reason := sdk.HealthUnavailable, "remote_unavailable"
		if remote.Category == sdk.ErrorUnauthorized || remote.Category == sdk.ErrorForbidden {
			status, reason = sdk.HealthDegraded, "auth_rejected"
		}
		if remote.Category == sdk.ErrorRateLimited {
			status, reason = sdk.HealthDegraded, "rate_limited"
		}
		return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: at}, nil
	}
	return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "remote_contract_invalid", CheckedAt: at}, nil
}
func normalizedTransportError() error {
	r, _ := sdk.NewRemoteError(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
	return r
}
func writeOutcomeUnknown() error {
	r, _ := sdk.NewRemoteError(sdk.ErrorConflict, "write_outcome_unknown", "", 0)
	return r
}
func normalizeHTTP(resp Response) error {
	if len(resp.Body) > maxBodyBytes || resp.RetryAfterMS < 0 {
		r, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return r
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	cat, code := sdk.ErrorTransient, "remote_error"
	switch resp.StatusCode {
	case 400, 405, 406, 415, 422:
		cat, code = sdk.ErrorInvalidRequest, "request_rejected"
	case 401:
		cat, code = sdk.ErrorUnauthorized, "auth_rejected"
	case 403:
		cat, code = sdk.ErrorForbidden, "access_denied"
	case 404:
		cat, code = sdk.ErrorNotFound, "resource_missing"
	case 409:
		cat, code = sdk.ErrorConflict, "remote_conflict"
	case 429:
		cat, code = sdk.ErrorRateLimited, "rate_limited"
	case 500, 502, 503, 504:
		cat, code = sdk.ErrorUnavailable, "remote_unavailable"
	}
	retry := time.Duration(0)
	if cat == sdk.ErrorRateLimited || cat == sdk.ErrorUnavailable || cat == sdk.ErrorTransient {
		retry = time.Duration(resp.RetryAfterMS) * time.Millisecond
	}
	r, err := sdk.NewRemoteError(cat, code, resp.RequestID, retry)
	if err != nil {
		r, _ = sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
	}
	return r
}
func isAmbiguousWrite(err error) bool {
	var remote *sdk.RemoteError
	return errors.As(err, &remote) && (remote.Category == sdk.ErrorUnavailable || remote.Category == sdk.ErrorTransient)
}
