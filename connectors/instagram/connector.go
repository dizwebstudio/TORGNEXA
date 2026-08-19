package instagram

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
	apiHost      = "graph.instagram.com"
	apiVersion   = "v26.0"
	maxBodyBytes = 12 << 20
)

var (
	ErrTransportMissing   = errors.New("instagram: transport missing")
	ErrInvalidResponse    = errors.New("instagram: invalid response")
	ErrInvalidCredentials = errors.New("instagram: invalid credentials")
	ErrMediaStagerMissing = errors.New("instagram: media stager missing")
)

type Param struct{ Name, Value string }
type Request struct {
	Method      string
	Host        string
	Path        string
	Params      []Param
	AccessToken []byte
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

func Manifest() sdk.Manifest                { manifest, _ := sdk.CatalogManifest("instagram"); return manifest }
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

func (c *Connector) configuration(ctx context.Context, account sdk.Account) (Configuration, error) {
	if c == nil || c.configs == nil {
		return Configuration{}, ErrConfigurationMissing
	}
	v, err := c.configs.Resolve(ctx, account)
	if err != nil || v.Validate() != nil {
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

func (c *Connector) withToken(ctx context.Context, runtime sdk.Runtime, ref sdk.SecretReference, cb func([]byte) error) error {
	if c == nil || runtime == nil || runtime.Secrets() == nil || cb == nil {
		return ErrInvalidCredentials
	}
	return runtime.Secrets().UseSecret(ctx, ref, func(secret []byte) error {
		if !validToken(secret) {
			return ErrInvalidCredentials
		}
		return cb(secret)
	})
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
	err = c.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		raw, callErr := c.call(ctx, token, "GET", "/"+apiVersion+"/"+cfg.InstagramUserID, []Param{{Name: "fields", Value: "id,username,account_type"}}, false)
		if callErr != nil {
			return callErr
		}
		var profile struct {
			ID          string `json:"id"`
			Username    string `json:"username"`
			AccountType string `json:"account_type"`
		}
		if json.Unmarshal(raw, &profile) != nil || profile.ID != cfg.InstagramUserID || strings.TrimSpace(profile.Username) == "" || (profile.AccountType != "BUSINESS" && profile.AccountType != "MEDIA_CREATOR" && profile.AccountType != "Business" && profile.AccountType != "Media_Creator") {
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
	if errors.Is(err, ErrInvalidResponse) || errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrInvalidConfiguration) {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "remote_contract_invalid", CheckedAt: at}, nil
	}
	return sdk.Health{}, err
}

type graphEnvelope struct {
	Error *struct {
		Code    int    `json:"code"`
		Subcode int    `json:"error_subcode"`
		Trace   string `json:"fbtrace_id"`
	} `json:"error"`
}

func (c *Connector) call(ctx context.Context, token []byte, method, path string, params []Param, write bool) (json.RawMessage, error) {
	if c == nil || c.transport == nil || !validToken(token) || (method != "GET" && method != "POST") || !strings.HasPrefix(path, "/"+apiVersion+"/") {
		return nil, ErrTransportMissing
	}
	response, err := c.transport.Do(ctx, Request{Method: method, Host: apiHost, Path: path, Params: append([]Param(nil), params...), AccessToken: append([]byte(nil), token...)})
	if err != nil {
		return nil, normalizedTransportError(write)
	}
	if len(response.Body) > maxBodyBytes || response.RetryAfterMS < 0 {
		return nil, newRemote(sdk.ErrorInternal, "response_invalid", "", 0)
	}
	var envelope graphEnvelope
	if len(response.Body) > 0 {
		_ = json.Unmarshal(response.Body, &envelope)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if envelope.Error != nil {
			return nil, normalizeGraphError(envelope.Error.Code, envelope.Error.Subcode, firstRequestID(response.RequestID, envelope.Error.Trace), response.RetryAfterMS, write)
		}
		return nil, normalizeHTTP(response, write)
	}
	if envelope.Error != nil {
		return nil, normalizeGraphError(envelope.Error.Code, envelope.Error.Subcode, firstRequestID(response.RequestID, envelope.Error.Trace), response.RetryAfterMS, write)
	}
	if len(response.Body) == 0 {
		return nil, ErrInvalidResponse
	}
	return append(json.RawMessage(nil), response.Body...), nil
}

func normalizedTransportError(write bool) error {
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
func normalizeGraphError(code, subcode int, requestID string, retryAfterMS int64, write bool) error {
	if write && (code == 1 || code == 2) {
		return newRemote(sdk.ErrorConflict, "write_outcome_unknown", requestID, 0)
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
	if subcode == 2207008 || subcode == 2207009 {
		cat, safe = sdk.ErrorInvalidRequest, "media_rejected"
	}
	return newRemote(cat, safe, requestID, retryAfterMS)
}
func newRemote(cat sdk.ErrorCategory, code, req string, retryMS int64) error {
	retry := time.Duration(0)
	if retryMS > 0 && (cat == sdk.ErrorRateLimited || cat == sdk.ErrorUnavailable || cat == sdk.ErrorTransient) {
		retry = time.Duration(retryMS) * time.Millisecond
	}
	e, err := sdk.NewRemoteError(cat, code, req, retry)
	if err != nil {
		fallback, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return fallback
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
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
