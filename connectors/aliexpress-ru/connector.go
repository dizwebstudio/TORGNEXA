package aliexpressru

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	apiHost      = "openapi.aliexpress.ru"
	productsPath = "/api/v1/scroll-short-product-by-filter"
	maxBodyBytes = 12 << 20
)

var (
	ErrTransportMissing   = errors.New("aliexpress-ru: transport missing")
	ErrInvalidResponse    = errors.New("aliexpress-ru: invalid response")
	ErrInvalidCredentials = errors.New("aliexpress-ru: invalid credentials")
)

type Request struct {
	Method     string
	Host       string
	Path       string
	Body       []byte
	XAuthToken []byte
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
	now       func() time.Time
}

func New(transport Transport, now func() time.Time) *Connector {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Connector{transport: transport, now: now}
}

func Manifest() sdk.Manifest { manifest, _ := sdk.CatalogManifest("aliexpress-ru"); return manifest }

func (connector *Connector) Manifest() sdk.Manifest { return Manifest() }

func (connector *Connector) withToken(ctx context.Context, runtime sdk.Runtime, reference sdk.SecretReference, callback func([]byte) error) error {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || callback == nil {
		return ErrInvalidCredentials
	}
	return runtime.Secrets().UseSecret(ctx, reference, func(secret []byte) error {
		if !validJWT(secret) {
			return ErrInvalidCredentials
		}
		return callback(secret)
	})
}

func validJWT(value []byte) bool {
	if len(value) < 24 || len(value) > 8192 || !utf8.Valid(value) || !bytes.Equal(value, bytes.TrimSpace(value)) {
		return false
	}
	parts := strings.Split(string(value), ".")
	if len(parts) != 3 {
		return false
	}
	for index, part := range parts {
		if part == "" || len(part) > 4096 {
			return false
		}
		decoded, err := base64.RawURLEncoding.DecodeString(part)
		if err != nil || len(decoded) == 0 {
			return false
		}
		if index < 2 && !utf8.Valid(decoded) {
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
	body := []byte(`{"filter":{},"limit":"1"}`)
	err := connector.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: productsPath, Body: body, XAuthToken: token})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		_, parseErr := parseProducts(response.Body, 1)
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
	return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "remote_contract_invalid", CheckedAt: checkedAt}, nil
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
	case 409:
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
		remote, _ = sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
	}
	return remote
}
