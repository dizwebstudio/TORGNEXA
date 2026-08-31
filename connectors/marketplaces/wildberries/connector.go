package wildberries

import (
	"context"
	"errors"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	contentHost     = "content-api.wildberries.ru"
	marketplaceHost = "marketplace-api.wildberries.ru"
	maxBodyBytes    = 8 << 20
)

var (
	ErrTransportMissing = errors.New("wildberries: transport missing")
	ErrInvalidResponse  = errors.New("wildberries: invalid response")
)

type Request struct {
	Method string
	Host   string
	Path   string
	Body   []byte
	Token  []byte
	// IdempotencyKey is non-secret host retry metadata. It is never persisted
	// by the connector and is forwarded only as a bounded request header.
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
	now       func() time.Time
}

func New(transport Transport, now func() time.Time) *Connector {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Connector{transport: transport, now: now}
}

func Manifest() sdk.Manifest { manifest, _ := sdk.CatalogManifest("wildberries"); return manifest }

func (connector *Connector) Manifest() sdk.Manifest { return Manifest() }

func (connector *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	checkedAt := connector.now().UTC()
	var result sdk.Health
	err := runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		for _, host := range []string{contentHost, marketplaceHost} {
			response, err := connector.transport.Do(ctx, Request{Method: "GET", Host: host, Path: "/ping", Token: secret})
			if err != nil {
				return normalizedTransportError()
			}
			if remote := normalizeHTTP(response); remote != nil {
				return remote
			}
		}
		result = sdk.Health{Status: sdk.HealthHealthy, CheckedAt: checkedAt}
		return nil
	})
	if err != nil {
		var remote *sdk.RemoteError
		if errors.As(err, &remote) {
			status := sdk.HealthUnavailable
			reason := "remote_unavailable"
			if remote.Category == sdk.ErrorUnauthorized || remote.Category == sdk.ErrorForbidden {
				status, reason = sdk.HealthDegraded, "auth_rejected"
			} else if remote.Category == sdk.ErrorRateLimited {
				status, reason = sdk.HealthDegraded, "rate_limited"
			}
			return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: checkedAt}, nil
		}
		return sdk.Health{}, err
	}
	return result, nil
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
	case 400, 406, 422:
		category, code = sdk.ErrorInvalidRequest, "request_rejected"
	case 401:
		category, code = sdk.ErrorUnauthorized, "auth_rejected"
	case 402, 403:
		category, code = sdk.ErrorForbidden, "access_denied"
	case 404:
		category, code = sdk.ErrorNotFound, "resource_not_found"
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
		fallback, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return fallback
	}
	return remote
}
