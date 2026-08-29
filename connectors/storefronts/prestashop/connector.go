package prestashop

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
	ErrTransportMissing   = errors.New("prestashop: transport missing")
	ErrInvalidResponse    = errors.New("prestashop: invalid response")
	ErrInvalidCredentials = errors.New("prestashop: invalid credentials")
)

type QueryParam struct{ Name, Value string }
type Request struct {
	Method   string
	Host     string
	Path     string
	Query    []QueryParam
	Body     []byte
	Username []byte
	Password []byte
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
	manifest, _ := sdk.CatalogManifest("prestashop")
	return manifest
}
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

func (c *Connector) configuration(ctx context.Context, account sdk.Account) (Configuration, error) {
	if c == nil || c.configs == nil {
		return Configuration{}, ErrConfigurationMissing
	}
	cfg, err := c.configs.Resolve(ctx, account)
	if err != nil || cfg.Validate() != nil {
		return Configuration{}, ErrInvalidConfiguration
	}
	return cfg, nil
}

type credentials struct {
	APIKey string `json:"api_key"`
}

func parseCredentials(raw []byte) (credentials, error) {
	if len(raw) < 20 || len(raw) > 4096 || !utf8.Valid(raw) || !bytes.Equal(raw, bytes.TrimSpace(raw)) {
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
	if len(v.APIKey) < 16 || len(v.APIKey) > 256 || v.APIKey != strings.TrimSpace(v.APIKey) {
		return credentials{}, ErrInvalidCredentials
	}
	for _, r := range v.APIKey {
		if r <= 0x20 || r == 0x7f {
			return credentials{}, ErrInvalidCredentials
		}
	}
	return v, nil
}
func (c *Connector) withCredentials(ctx context.Context, runtime sdk.Runtime, ref sdk.SecretReference, cb func(credentials) error) error {
	if c == nil || runtime == nil || runtime.Secrets() == nil || cb == nil {
		return ErrInvalidCredentials
	}
	return runtime.Secrets().UseSecret(ctx, ref, func(secret []byte) error {
		v, err := parseCredentials(secret)
		if err != nil {
			return err
		}
		defer func() { v.APIKey = "" }()
		return cb(v)
	})
}
func mergeQuery(a, b []QueryParam) []QueryParam {
	out := make([]QueryParam, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}
func (c *Connector) call(ctx context.Context, cfg Configuration, cred credentials, method, path string, query []QueryParam, body []byte) (Response, error) {
	if c == nil || c.transport == nil {
		return Response{}, ErrTransportMissing
	}
	user := []byte(cred.APIKey)
	defer clear(user)
	resp, err := c.transport.Do(ctx, Request{Method: method, Host: cfg.StoreHost, Path: cfg.apiPath(path), Query: mergeQuery(cfg.commonQuery(), query), Body: body, Username: user})
	if err != nil {
		return Response{}, normalizedTransportError()
	}
	if remote := normalizeHTTP(resp); remote != nil {
		return resp, remote
	}
	return resp, nil
}
func (c *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	at := c.now().UTC()
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "configuration_invalid", CheckedAt: at}, nil
	}
	err = c.withCredentials(ctx, runtime, account.SecretReference, func(cred credentials) error {
		resp, e := c.call(ctx, cfg, cred, "GET", "/products", []QueryParam{{Name: "display", Value: "[id]"}, {Name: "limit", Value: "1"}}, nil)
		if e != nil {
			return e
		}
		var env struct {
			Products []map[string]any `json:"products"`
		}
		if json.Unmarshal(resp.Body, &env) != nil || len(env.Products) > 1 {
			return ErrInvalidResponse
		}
		return nil
	})
	if err == nil {
		return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: at}, nil
	}
	var remote *sdk.RemoteError
	if errors.As(err, &remote) {
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
