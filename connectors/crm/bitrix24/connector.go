package bitrix24

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const maxBodyBytes = 16 << 20

var (
	ErrTransportMissing   = errors.New("bitrix24: transport missing")
	ErrInvalidResponse    = errors.New("bitrix24: invalid response")
	ErrInvalidCredentials = errors.New("bitrix24: invalid credentials")
)

type Request struct {
	Method string
	Host   string
	Path   string
	Body   []byte
	Bearer []byte
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

func Manifest() sdk.Manifest                { manifest, _ := sdk.CatalogManifest("bitrix24"); return manifest }
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

type credentials struct {
	AccessToken string
}

func parseCredentials(raw []byte) (credentials, error) {
	if len(raw) < 16 || len(raw) > 4096 || !utf8.Valid(raw) || !bytes.Equal(raw, bytes.TrimSpace(raw)) {
		return credentials{}, ErrInvalidCredentials
	}
	value := string(raw)
	if value != strings.TrimSpace(value) {
		return credentials{}, ErrInvalidCredentials
	}
	for _, r := range value {
		if r <= 0x20 || r == 0x7f {
			return credentials{}, ErrInvalidCredentials
		}
	}
	return credentials{AccessToken: value}, nil
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
		defer func() { v.AccessToken = "" }()
		return cb(v)
	})
}
func (c *Connector) configuration(ctx context.Context, a sdk.Account) (Configuration, error) {
	if c == nil || c.configs == nil {
		return Configuration{}, ErrConfigurationMissing
	}
	cfg, e := c.configs.Resolve(ctx, a)
	if e != nil || cfg.Validate() != nil {
		return Configuration{}, ErrInvalidConfiguration
	}
	return cfg, nil
}

func (c *Connector) call(ctx context.Context, cfg Configuration, cred credentials, method string, body any) (Response, error) {
	if c == nil || c.transport == nil {
		return Response{}, ErrTransportMissing
	}
	payload, e := json.Marshal(body)
	if e != nil {
		return Response{}, ErrInvalidResponse
	}
	token := []byte(cred.AccessToken)
	defer clear(token)
	resp, e := c.transport.Do(ctx, Request{Method: "POST", Host: cfg.PortalHost, Path: "/rest/" + method, Body: payload, Bearer: token})
	if e != nil {
		return Response{}, normalizedTransportError()
	}
	if e = normalizeHTTP(resp); e != nil {
		return resp, e
	}
	if e = normalizeAPIError(resp); e != nil {
		return resp, e
	}
	return resp, nil
}

func normalizeAPIError(resp Response) error {
	var env struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(resp.Body, &env) != nil {
		return ErrInvalidResponse
	}
	if env.Error == "" {
		return nil
	}
	cat, code := sdk.ErrorInvalidRequest, "request_rejected"
	upper := strings.ToUpper(env.Error)
	switch {
	case strings.Contains(upper, "EXPIRED_TOKEN") || strings.Contains(upper, "INVALID_TOKEN") || strings.Contains(upper, "NO_AUTH"):
		cat, code = sdk.ErrorUnauthorized, "auth_rejected"
	case strings.Contains(upper, "ACCESS_DENIED") || strings.Contains(upper, "INSUFFICIENT_SCOPE") || strings.Contains(upper, "FORBIDDEN"):
		cat, code = sdk.ErrorForbidden, "access_denied"
	case strings.Contains(upper, "QUERY_LIMIT") || strings.Contains(upper, "TOO_MANY"):
		cat, code = sdk.ErrorRateLimited, "rate_limited"
	case strings.Contains(upper, "NOT_FOUND"):
		cat, code = sdk.ErrorNotFound, "resource_missing"
	}
	r, _ := sdk.NewRemoteError(cat, code, resp.RequestID, time.Duration(resp.RetryAfterMS)*time.Millisecond)
	return r
}
func normalizeHTTP(resp Response) error {
	if len(resp.Body) > maxBodyBytes || resp.RetryAfterMS < 0 {
		return ErrInvalidResponse
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
	r, _ := sdk.NewRemoteError(cat, code, resp.RequestID, retry)
	return r
}
func normalizedTransportError() error {
	r, _ := sdk.NewRemoteError(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
	return r
}
func writeOutcomeUnknown() error {
	r, _ := sdk.NewRemoteError(sdk.ErrorConflict, "write_outcome_unknown", "", 0)
	return r
}
func isAmbiguousWrite(e error) bool {
	var r *sdk.RemoteError
	return errors.As(e, &r) && (r.Category == sdk.ErrorUnavailable || r.Category == sdk.ErrorTransient || r.Category == sdk.ErrorRateLimited)
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
		resp, e := c.call(ctx, cfg, cred, "crm.item.fields", map[string]any{"entityTypeId": 2})
		if e != nil {
			return e
		}
		var env struct {
			Result struct {
				Fields map[string]any `json:"fields"`
			} `json:"result"`
		}
		if json.Unmarshal(resp.Body, &env) != nil || len(env.Result.Fields) == 0 {
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
