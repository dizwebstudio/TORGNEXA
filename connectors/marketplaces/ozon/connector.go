package ozon

import (
	"bytes"
	"context"
	"errors"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	apiHost      = "api-seller.ozon.ru"
	maxBodyBytes = 8 << 20
)

var (
	ErrTransportMissing   = errors.New("ozon: transport missing")
	ErrInvalidResponse    = errors.New("ozon: invalid response")
	ErrInvalidCredentials = errors.New("ozon: invalid credential bundle")
)

type Request struct {
	Method         string
	Host           string
	Path           string
	Query          []QueryParam
	Body           []byte
	ClientID       []byte
	APIKey         []byte
	Bearer         []byte
	IdempotencyKey string
}

type QueryParam struct{ Name, Value string }

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

func Manifest() sdk.Manifest { manifest, _ := sdk.CatalogManifest("ozon"); return manifest }

func (connector *Connector) Manifest() sdk.Manifest { return Manifest() }

func (connector *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	checkedAt := connector.now().UTC()
	body := []byte(`{"filter":{"visibility":"ALL"},"last_id":"","limit":1}`)
	err := connector.withCredentials(ctx, runtime, account.SecretReference, func(clientID, apiKey []byte) error {
		response, callErr := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/v3/product/list", Body: body, ClientID: clientID, APIKey: apiKey})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		_, parseErr := parseProductList(response.Body, 1, "")
		return parseErr
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
		if errors.Is(err, ErrInvalidResponse) || errors.Is(err, ErrInvalidCredentials) {
			return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "remote_contract_invalid", CheckedAt: checkedAt}, nil
		}
		return sdk.Health{}, err
	}
	return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: checkedAt}, nil
}

func (connector *Connector) withCredentials(ctx context.Context, runtime sdk.Runtime, reference sdk.SecretReference, callback func([]byte, []byte) error) error {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || callback == nil {
		return ErrInvalidCredentials
	}
	return runtime.Secrets().UseSecret(ctx, reference, func(secret []byte) error {
		clientID, apiKey, err := parseCredentialBundle(secret)
		if err != nil {
			return err
		}
		return callback(clientID, apiKey)
	})
}

// Credential bundle format is two ASCII lines: Client-Id, then Api-Key. The
// slices point into the secret-provider buffer so provider code does not retain
// a second plaintext credential copy beyond the scoped callback.
func parseCredentialBundle(secret []byte) ([]byte, []byte, error) {
	if len(secret) < 3 || len(secret) > 2048 || bytes.Count(secret, []byte{'\n'}) != 1 {
		return nil, nil, ErrInvalidCredentials
	}
	parts := bytes.SplitN(secret, []byte{'\n'}, 2)
	clientID, apiKey := parts[0], parts[1]
	if len(clientID) < 1 || len(clientID) > 32 || len(apiKey) < 8 || len(apiKey) > 1024 {
		return nil, nil, ErrInvalidCredentials
	}
	for _, b := range clientID {
		if b < '0' || b > '9' {
			return nil, nil, ErrInvalidCredentials
		}
	}
	for _, b := range apiKey {
		if b < 0x21 || b > 0x7e {
			return nil, nil, ErrInvalidCredentials
		}
	}
	return clientID, apiKey, nil
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
	case 401, 404:
		// Ozon documents API-key failures that may surface as 404 on Seller API.
		category, code = sdk.ErrorUnauthorized, "auth_rejected"
	case 402, 403:
		category, code = sdk.ErrorForbidden, "access_denied"
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
