package moysklad

import (
	"bytes"
	"context"
	"errors"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	apiHost      = "api.moysklad.ru"
	apiBasePath  = "/api/remap/1.2"
	maxBodyBytes = 20 << 20
	maxPageLimit = 1000
)

var (
	ErrTransportMissing   = errors.New("moysklad: transport missing")
	ErrInvalidResponse    = errors.New("moysklad: invalid response")
	ErrInvalidCredentials = errors.New("moysklad: invalid credentials")
)

type QueryParam struct{ Name, Value string }

type Request struct {
	Method     string
	Host       string
	Path       string
	Query      []QueryParam
	Token      []byte
	AcceptGzip bool
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

func Manifest() sdk.Manifest                { manifest, _ := sdk.CatalogManifest("moysklad"); return manifest }
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

func (c *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	checked := c.now().UTC()
	err := c.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		response, callErr := c.transport.Do(ctx, Request{Method: "GET", Host: apiHost, Path: apiBasePath + "/entity/assortment", Query: []QueryParam{{Name: "limit", Value: "1"}, {Name: "offset", Value: "0"}, {Name: "groupBy", Value: "product"}}, Token: token, AcceptGzip: true})
		if callErr != nil {
			return normalizedTransportError()
		}
		if remote := normalizeHTTP(response); remote != nil {
			return remote
		}
		_, err := decodeListEnvelope(response.Body, 1, 0)
		return err
	})
	if err == nil {
		return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: checked}, nil
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
		return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: checked}, nil
	}
	if errors.Is(err, ErrInvalidResponse) || errors.Is(err, ErrInvalidCredentials) {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "remote_contract_invalid", CheckedAt: checked}, nil
	}
	return sdk.Health{}, err
}

func (c *Connector) withToken(ctx context.Context, runtime sdk.Runtime, reference sdk.SecretReference, callback func([]byte) error) error {
	if c == nil || runtime == nil || runtime.Secrets() == nil || callback == nil {
		return ErrInvalidCredentials
	}
	return runtime.Secrets().UseSecret(ctx, reference, func(secret []byte) error {
		if !validToken(secret) {
			return ErrInvalidCredentials
		}
		return callback(secret)
	})
}

func validToken(value []byte) bool {
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
