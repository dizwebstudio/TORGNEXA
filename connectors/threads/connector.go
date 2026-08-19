package threads

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

const (
	apiHost      = "graph.threads.net"
	apiVersion   = "v1.0"
	maxBodyBytes = 12 << 20
)

var (
	ErrTransportMissing   = errors.New("threads: transport missing")
	ErrInvalidResponse    = errors.New("threads: invalid response")
	ErrInvalidCredentials = errors.New("threads: invalid credentials")
)

type Param struct{ Name, Value string }
type Request struct {
	Method, Host, Path string
	Params             []Param
	AccessToken        []byte
	AppSecret          []byte
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
	stager    MediaStager
	now       func() time.Time
	wait      func(context.Context, time.Duration) error
}

func New(transport Transport, configs ConfigurationSource, stager MediaStager, now func() time.Time) *Connector {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Connector{transport: transport, configs: configs, stager: stager, now: now, wait: waitContext}
}
func Manifest() sdk.Manifest                { manifest, _ := sdk.CatalogManifest("threads"); return manifest }
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }
func (c *Connector) configuration(ctx context.Context, a sdk.Account) (Configuration, error) {
	if c == nil || c.configs == nil {
		return Configuration{}, ErrConfigurationMissing
	}
	v, e := c.configs.Resolve(ctx, a)
	if e != nil || v.Validate() != nil {
		return Configuration{}, ErrInvalidConfiguration
	}
	return v, nil
}
func validToken(v []byte) bool {
	if len(v) < 16 || len(v) > 8192 || !utf8.Valid(v) || !bytes.Equal(v, bytes.TrimSpace(v)) {
		return false
	}
	for _, b := range v {
		if b <= 0x20 || b == 0x7f {
			return false
		}
	}
	return true
}
func validAppSecret(v []byte) bool {
	if len(v) < 16 || len(v) > 256 || !utf8.Valid(v) || !bytes.Equal(v, bytes.TrimSpace(v)) {
		return false
	}
	for _, b := range v {
		if b <= 0x20 || b == 0x7f {
			return false
		}
	}
	return true
}
func (c *Connector) useSecret(ctx context.Context, r sdk.Runtime, ref sdk.SecretReference, validator func([]byte) bool, cb func([]byte) error) error {
	if c == nil || r == nil || r.Secrets() == nil || ref == "" || validator == nil || cb == nil {
		return ErrInvalidCredentials
	}
	return r.Secrets().UseSecret(ctx, ref, func(s []byte) error {
		if !validator(s) {
			return ErrInvalidCredentials
		}
		return cb(s)
	})
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
	e = c.useSecret(ctx, r, a.SecretReference, validToken, func(token []byte) error {
		raw, ce := c.call(ctx, token, "GET", "/"+apiVersion+"/me", []Param{{Name: "fields", Value: "id,username"}}, false)
		if ce != nil {
			return ce
		}
		var p struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		}
		if json.Unmarshal(raw, &p) != nil || p.ID != cfg.ThreadsUserID || strings.TrimSpace(p.Username) == "" {
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
		switch remote.Category {
		case sdk.ErrorUnauthorized:
			status, reason = sdk.HealthDegraded, "auth_rejected"
		case sdk.ErrorForbidden:
			status, reason = sdk.HealthDegraded, "content_publish_permission_missing"
		case sdk.ErrorRateLimited:
			status, reason = sdk.HealthDegraded, "rate_limited"
		}
		return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: at}, nil
	}
	if errors.Is(e, ErrInvalidResponse) || errors.Is(e, ErrInvalidCredentials) || errors.Is(e, ErrInvalidConfiguration) {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "remote_contract_invalid", CheckedAt: at}, nil
	}
	return sdk.Health{}, e
}

type graphEnvelope struct {
	Error *struct {
		Code    int    `json:"code"`
		Subcode int    `json:"error_subcode"`
		Trace   string `json:"fbtrace_id"`
	} `json:"error"`
}

func (c *Connector) call(ctx context.Context, token []byte, method, path string, params []Param, write bool) (json.RawMessage, error) {
	if c == nil || c.transport == nil || !validToken(token) || (method != "GET" && method != "POST") || (!strings.HasPrefix(path, "/"+apiVersion+"/") && path != "/access_token" && path != "/refresh_access_token") {
		return nil, ErrTransportMissing
	}
	return c.do(ctx, Request{Method: method, Host: apiHost, Path: path, Params: append([]Param(nil), params...), AccessToken: append([]byte(nil), token...)}, write)
}

func (c *Connector) do(ctx context.Context, request Request, write bool) (json.RawMessage, error) {
	resp, e := c.transport.Do(ctx, request)
	if e != nil {
		return nil, transportError(write)
	}
	if len(resp.Body) > maxBodyBytes || resp.RetryAfterMS < 0 {
		return nil, newRemote(sdk.ErrorInternal, "response_invalid", "", 0)
	}
	var env graphEnvelope
	if len(resp.Body) > 0 {
		_ = json.Unmarshal(resp.Body, &env)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if env.Error != nil {
			return nil, normalizeGraphError(env.Error.Code, env.Error.Subcode, firstRequestID(resp.RequestID, env.Error.Trace), resp.RetryAfterMS, write)
		}
		return nil, normalizeHTTP(resp, write)
	}
	if env.Error != nil {
		return nil, normalizeGraphError(env.Error.Code, env.Error.Subcode, firstRequestID(resp.RequestID, env.Error.Trace), resp.RetryAfterMS, write)
	}
	if len(resp.Body) == 0 {
		return nil, ErrInvalidResponse
	}
	return append(json.RawMessage(nil), resp.Body...), nil
}
func transportError(write bool) error {
	if write {
		return newRemote(sdk.ErrorConflict, "write_outcome_unknown", "", 0)
	}
	return newRemote(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
}
func normalizeHTTP(r Response, write bool) error {
	if write && r.StatusCode >= 500 {
		return newRemote(sdk.ErrorConflict, "write_outcome_unknown", r.RequestID, 0)
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
	case 409:
		cat, code = sdk.ErrorConflict, "remote_conflict"
	case 429:
		cat, code = sdk.ErrorRateLimited, "rate_limited"
	case 500, 502, 503, 504:
		cat, code = sdk.ErrorUnavailable, "remote_unavailable"
	}
	return newRemote(cat, code, r.RequestID, r.RetryAfterMS)
}
func normalizeGraphError(code, sub int, req string, retryMS int64, write bool) error {
	if write && (code == 1 || code == 2) {
		return newRemote(sdk.ErrorConflict, "write_outcome_unknown", req, 0)
	}
	cat, safe := sdk.ErrorInvalidRequest, "request_rejected"
	switch code {
	case 190:
		cat, safe = sdk.ErrorUnauthorized, "auth_rejected"
	case 10, 200:
		cat, safe = sdk.ErrorForbidden, "access_denied"
	case 4, 17, 32, 613:
		cat, safe = sdk.ErrorRateLimited, "rate_limited"
	case 1, 2:
		cat, safe = sdk.ErrorUnavailable, "remote_unavailable"
	case 100:
		cat, safe = sdk.ErrorInvalidRequest, "invalid_parameters"
	case 368:
		cat, safe = sdk.ErrorForbidden, "content_rejected"
	}
	if sub != 0 && code == 100 {
		safe = "media_rejected"
	}
	return newRemote(cat, safe, req, retryMS)
}
func newRemote(cat sdk.ErrorCategory, code, req string, retryMS int64) error {
	d := time.Duration(0)
	if retryMS > 0 && (cat == sdk.ErrorRateLimited || cat == sdk.ErrorUnavailable || cat == sdk.ErrorTransient) {
		d = time.Duration(retryMS) * time.Millisecond
	}
	e, err := sdk.NewRemoteError(cat, code, req, d)
	if err != nil {
		f, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return f
	}
	return e
}
func firstRequestID(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
func waitContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	tm := time.NewTimer(d)
	defer tm.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-tm.C:
		return nil
	}
}
