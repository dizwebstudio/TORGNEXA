package cscart

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
	ErrTransportMissing   = errors.New("cs-cart: transport missing")
	ErrInvalidResponse    = errors.New("cs-cart: invalid response")
	ErrInvalidCredentials = errors.New("cs-cart: invalid credentials")
	ErrProductNotFound    = errors.New("cs-cart: product not found")
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
	manifest, _ := sdk.CatalogManifest("cs-cart")
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
	Email  string `json:"email"`
	APIKey string `json:"api_key"`
}

func parseCredentials(raw []byte) (credentials, error) {
	if len(raw) < 32 || len(raw) > 8192 || !utf8.Valid(raw) || !bytes.Equal(raw, bytes.TrimSpace(raw)) {
		return credentials{}, ErrInvalidCredentials
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value credentials
	if decoder.Decode(&value) != nil {
		return credentials{}, ErrInvalidCredentials
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return credentials{}, ErrInvalidCredentials
	}
	if !validCredentialText(value.Email, 5, 320) || !strings.Contains(value.Email, "@") || !validCredentialText(value.APIKey, 16, 4096) {
		return credentials{}, ErrInvalidCredentials
	}
	return value, nil
}

func validCredentialText(value string, min, max int) bool {
	if len(value) < min || len(value) > max || value != strings.TrimSpace(value) || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func (c *Connector) withCredentials(ctx context.Context, runtime sdk.Runtime, ref sdk.SecretReference, callback func(credentials) error) error {
	if c == nil || runtime == nil || runtime.Secrets() == nil || callback == nil {
		return ErrInvalidCredentials
	}
	return runtime.Secrets().UseSecret(ctx, ref, func(secret []byte) error {
		value, err := parseCredentials(secret)
		if err != nil {
			return err
		}
		defer func() { value.Email, value.APIKey = "", "" }()
		return callback(value)
	})
}

func (c *Connector) call(ctx context.Context, cfg Configuration, cred credentials, method, path string, query []QueryParam, body []byte) (Response, error) {
	if c == nil || c.transport == nil {
		return Response{}, ErrTransportMissing
	}
	user, pass := []byte(cred.Email), []byte(cred.APIKey)
	defer clear(user)
	defer clear(pass)
	resp, err := c.transport.Do(ctx, Request{Method: method, Host: cfg.StoreHost, Path: cfg.apiPath(path), Query: query, Body: body, Username: user, Password: pass})
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
		_, callErr := c.call(ctx, cfg, cred, "GET", "/products", []QueryParam{{Name: "page", Value: "1"}, {Name: "items_per_page", Value: "1"}}, nil)
		return callErr
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
	remote, _ := sdk.NewRemoteError(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
	return remote
}

func writeOutcomeUnknown() error {
	remote, _ := sdk.NewRemoteError(sdk.ErrorConflict, "write_outcome_unknown", "", 0)
	return remote
}

func isAmbiguousWrite(err error) bool {
	var remote *sdk.RemoteError
	return errors.As(err, &remote) && (remote.Category == sdk.ErrorUnavailable || remote.Category == sdk.ErrorTransient || remote.Category == sdk.ErrorTimeout)
}

func normalizeHTTP(resp Response) error {
	if len(resp.Body) > maxBodyBytes || resp.RetryAfterMS < 0 {
		remote, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return remote
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	category, code := sdk.ErrorTransient, "remote_error"
	switch resp.StatusCode {
	case 400, 405, 406, 415, 422:
		category, code = sdk.ErrorInvalidRequest, "request_rejected"
	case 401:
		category, code = sdk.ErrorUnauthorized, "auth_rejected"
	case 403:
		category, code = sdk.ErrorForbidden, "access_denied"
	case 404:
		category, code = sdk.ErrorNotFound, "resource_missing"
	case 409:
		category, code = sdk.ErrorConflict, "remote_conflict"
	case 429:
		category, code = sdk.ErrorRateLimited, "rate_limited"
	case 500, 502, 503, 504:
		category, code = sdk.ErrorUnavailable, "remote_unavailable"
	}
	retry := time.Duration(0)
	if category == sdk.ErrorRateLimited || category == sdk.ErrorUnavailable || category == sdk.ErrorTransient {
		retry = time.Duration(resp.RetryAfterMS) * time.Millisecond
	}
	remote, err := sdk.NewRemoteError(category, code, resp.RequestID, retry)
	if err != nil {
		remote, _ = sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
	}
	return remote
}
