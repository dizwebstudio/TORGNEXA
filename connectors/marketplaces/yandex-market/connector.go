package yandexmarket

import (
	"bytes"
	"context"
	"errors"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	apiHost      = "api.partner.market.yandex.ru"
	maxBodyBytes = 12 << 20
)

var (
	ErrTransportMissing   = errors.New("yandexmarket: transport missing")
	ErrInvalidResponse    = errors.New("yandexmarket: invalid response")
	ErrInvalidCredentials = errors.New("yandexmarket: invalid credentials")
)

type QueryParam struct {
	Name  string
	Value string
}

type Request struct {
	Method         string
	Host           string
	Path           string
	Query          []QueryParam
	Body           []byte
	APIKey         []byte
	IdempotencyKey string
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

func Manifest() sdk.Manifest { manifest, _ := sdk.CatalogManifest("yandex-market"); return manifest }

func (connector *Connector) Manifest() sdk.Manifest { return Manifest() }

func (connector *Connector) configuration(ctx context.Context, account sdk.Account) (Configuration, error) {
	if connector == nil || connector.configs == nil {
		return Configuration{}, ErrConfigurationMissing
	}
	configuration, err := connector.configs.Resolve(ctx, account)
	if err != nil {
		return Configuration{}, ErrConfigurationMissing
	}
	if err := configuration.Validate(); err != nil {
		return Configuration{}, err
	}
	return configuration, nil
}

func (connector *Connector) withAPIKey(ctx context.Context, runtime sdk.Runtime, reference sdk.SecretReference, callback func([]byte) error) error {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || callback == nil {
		return ErrInvalidCredentials
	}
	return runtime.Secrets().UseSecret(ctx, reference, func(secret []byte) error {
		if !validAPIKey(secret) {
			return ErrInvalidCredentials
		}
		return callback(secret)
	})
}

func validAPIKey(value []byte) bool {
	if len(value) < 16 || len(value) > 4096 || !utf8.Valid(value) || !bytes.Equal(value, bytes.TrimSpace(value)) {
		return false
	}
	for _, b := range value {
		if b <= 0x20 || b == 0x7f {
			return false
		}
	}
	return true
}

func (connector *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	checkedAt := connector.now().UTC()
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "configuration_invalid", CheckedAt: checkedAt}, nil
	}
	body := []byte(`{}`)
	err = connector.withAPIKey(ctx, runtime, account.SecretReference, func(key []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: businessPath(configuration.BusinessID, "/offer-mappings"), Query: []QueryParam{{Name: "limit", Value: "1"}}, Body: body, APIKey: key})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		_, parseErr := parseProducts(response.Body, 1, configuration, "")
		return parseErr
	})
	if err == nil {
		return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: checkedAt}, nil
	}
	var remote *sdk.RemoteError
	if errors.As(err, &remote) {
		status, reason := sdk.HealthUnavailable, "remote_unavailable"
		if remote.Category == sdk.ErrorUnauthorized || remote.Category == sdk.ErrorForbidden {
			status, reason = sdk.HealthDegraded, "auth_rejected"
		} else if remote.Category == sdk.ErrorRateLimited {
			status, reason = sdk.HealthDegraded, "rate_limited"
		}
		return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: checkedAt}, nil
	}
	if errors.Is(err, ErrInvalidResponse) || errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrInvalidConfiguration) || errors.Is(err, ErrConfigurationMissing) {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "remote_contract_invalid", CheckedAt: checkedAt}, nil
	}
	return sdk.Health{}, err
}

func normalizedTransportError() error {
	remote, _ := sdk.NewRemoteError(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
	return remote
}

func normalizeHTTP(response Response) error {
	if len(response.Body) > maxBodyBytes || response.RetryAfterMS < 0 {
		remote, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return remote
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	category, code := sdk.ErrorTransient, "remote_error"
	switch response.StatusCode {
	case 400, 405, 406, 415, 422:
		category, code = sdk.ErrorInvalidRequest, "request_rejected"
	case 401:
		category, code = sdk.ErrorUnauthorized, "auth_rejected"
	case 403:
		category, code = sdk.ErrorForbidden, "access_denied"
	case 404:
		category, code = sdk.ErrorNotFound, "resource_missing"
	case 409, 423:
		category, code = sdk.ErrorConflict, "remote_conflict"
	case 420, 429:
		category, code = sdk.ErrorRateLimited, "rate_limited"
	case 500, 502, 503, 504:
		category, code = sdk.ErrorUnavailable, "remote_unavailable"
	}
	retry := time.Duration(0)
	if category == sdk.ErrorRateLimited || category == sdk.ErrorUnavailable || category == sdk.ErrorTransient {
		retry = time.Duration(response.RetryAfterMS) * time.Millisecond
	}
	remote, err := sdk.NewRemoteError(category, code, response.RequestID, retry)
	if err != nil {
		fallback, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return fallback
	}
	return remote
}
