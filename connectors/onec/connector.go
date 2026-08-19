package onec

import (
	"bytes"
	"context"
	"errors"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const maxBodyBytes = 8 << 20

var (
	ErrTransportMissing   = errors.New("onec: transport missing")
	ErrInvalidResponse    = errors.New("onec: invalid response")
	ErrInvalidCredentials = errors.New("onec: invalid credential bundle")
)

type QueryParam struct {
	Name  string
	Value string
}

type Request struct {
	Method   string
	Host     string
	Path     string
	Query    []QueryParam
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
	return sdk.Manifest{
		ID: "onec", Name: "1C", Family: sdk.FamilyERP, Version: "1.0.0", SDKVersion: sdk.SDKMajor,
		Capabilities: []sdk.Capability{"erp.catalog.read", "erp.inventory.read"},
		Auth:         []sdk.AuthRequirement{{Kind: sdk.AuthBasic, SecretClass: "erp.basic-credentials", Required: true}},
		RateLimit: sdk.RateLimitPolicy{MaxConcurrency: 2, MinIntervalMS: 100, RequestTimeoutMS: 20000,
			Retry: sdk.RetryPolicy{MaxAttempts: 5, BaseBackoffMS: 500, MaxBackoffMS: 30000}},
	}
}

func (connector *Connector) Manifest() sdk.Manifest { return Manifest() }

func (connector *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if connector == nil || connector.transport == nil || connector.configs == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	checkedAt := connector.now().UTC()
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "configuration_invalid", CheckedAt: checkedAt}, nil
	}
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(username, password []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "GET", Host: configuration.Host, Path: configuration.BasePath + "/$metadata", Username: username, Password: password})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		if !validMetadataBody(response.Body) {
			return ErrInvalidResponse
		}
		return nil
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

func (connector *Connector) withCredentials(ctx context.Context, runtime sdk.Runtime, reference sdk.SecretReference, callback func([]byte, []byte) error) error {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || callback == nil {
		return ErrInvalidCredentials
	}
	return runtime.Secrets().UseSecret(ctx, reference, func(secret []byte) error {
		username, password, err := parseCredentialBundle(secret)
		if err != nil {
			return err
		}
		return callback(username, password)
	})
}

func parseCredentialBundle(secret []byte) ([]byte, []byte, error) {
	if len(secret) < 3 || len(secret) > 4096 || bytes.Count(secret, []byte{'\n'}) != 1 || !utf8.Valid(secret) {
		return nil, nil, ErrInvalidCredentials
	}
	parts := bytes.SplitN(secret, []byte{'\n'}, 2)
	if len(parts[0]) < 1 || len(parts[0]) > 256 || len(parts[1]) < 1 || len(parts[1]) > 3072 || unsafeCredential(parts[0]) || unsafeCredential(parts[1]) {
		return nil, nil, ErrInvalidCredentials
	}
	return parts[0], parts[1], nil
}

func unsafeCredential(value []byte) bool {
	for _, b := range value {
		if b == 0 || b == '\r' || b == '\n' || b < 0x20 || b == 0x7f {
			return true
		}
	}
	return false
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
	case 409, 412:
		category, code = sdk.ErrorConflict, "remote_conflict"
	case 429:
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

func validMetadataBody(body []byte) bool {
	if len(body) == 0 || len(body) > maxBodyBytes {
		return false
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '<' {
		return false
	}
	return bytes.Contains(trimmed, []byte("Edmx")) && !bytes.Contains(bytes.ToLower(trimmed), []byte("<html"))
}
