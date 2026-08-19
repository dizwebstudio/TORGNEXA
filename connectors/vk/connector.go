package vk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	apiHost      = "api.vk.com"
	apiVersion   = "5.199"
	maxBodyBytes = 12 << 20
)

var (
	ErrTransportMissing   = errors.New("vk: transport missing")
	ErrInvalidResponse    = errors.New("vk: invalid response")
	ErrInvalidCredentials = errors.New("vk: invalid credentials")
)

type Param struct {
	Name  string
	Value string
}

type Request struct {
	Method      string
	Host        string
	APIMethod   string
	Params      []Param
	AccessToken []byte
}

type UploadRequest struct {
	URL       string
	FieldName string
	FileName  string
	MediaType string
	SizeBytes int64
	SHA256    string
	Body      io.Reader
}

type Response struct {
	StatusCode   int
	Body         []byte
	RequestID    string
	RetryAfterMS int64
}

type Transport interface {
	Do(context.Context, Request) (Response, error)
	Upload(context.Context, UploadRequest) (Response, error)
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

func Manifest() sdk.Manifest { manifest, _ := sdk.CatalogManifest("vk"); return manifest }

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

func (connector *Connector) withToken(ctx context.Context, runtime sdk.Runtime, reference sdk.SecretReference, callback func([]byte) error) error {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || callback == nil {
		return ErrInvalidCredentials
	}
	return runtime.Secrets().UseSecret(ctx, reference, func(secret []byte) error {
		if !validAccessToken(secret) {
			return ErrInvalidCredentials
		}
		return callback(secret)
	})
}

func validAccessToken(value []byte) bool {
	if len(value) < 16 || len(value) > 8192 || !utf8.Valid(value) || !bytes.Equal(value, bytes.TrimSpace(value)) {
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
	err = connector.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		_, callErr := connector.call(ctx, token, "groups.getById", []Param{{Name: "group_ids", Value: strconv.FormatInt(configuration.GroupID, 10)}})
		return callErr
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

type vkEnvelope struct {
	Response json.RawMessage `json:"response"`
	Error    *struct {
		Code int `json:"error_code"`
	} `json:"error"`
}

func (connector *Connector) call(ctx context.Context, token []byte, method string, params []Param) (json.RawMessage, error) {
	if connector == nil || connector.transport == nil || method == "" || !validAccessToken(token) {
		return nil, ErrTransportMissing
	}
	requestParams := append([]Param(nil), params...)
	requestParams = append(requestParams, Param{Name: "v", Value: apiVersion})
	response, err := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, APIMethod: method, Params: requestParams, AccessToken: token})
	if err != nil {
		return nil, normalizedTransportError()
	}
	if remote := normalizeHTTP(response); remote != nil {
		return nil, remote
	}
	var envelope vkEnvelope
	if len(response.Body) == 0 || json.Unmarshal(response.Body, &envelope) != nil {
		return nil, ErrInvalidResponse
	}
	if envelope.Error != nil {
		return nil, normalizeVKError(envelope.Error.Code, response.RequestID, response.RetryAfterMS)
	}
	if len(envelope.Response) == 0 || bytes.Equal(envelope.Response, []byte("null")) {
		return nil, ErrInvalidResponse
	}
	return append(json.RawMessage(nil), envelope.Response...), nil
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
	return newRemote(category, code, response.RequestID, response.RetryAfterMS)
}

func normalizeVKError(code int, requestID string, retryAfterMS int64) error {
	category, safeCode := sdk.ErrorInvalidRequest, "request_rejected"
	switch code {
	case 5, 27:
		category, safeCode = sdk.ErrorUnauthorized, "auth_rejected"
	case 6, 29:
		category, safeCode = sdk.ErrorRateLimited, "rate_limited"
	case 7, 15, 210, 211, 212, 213, 214:
		category, safeCode = sdk.ErrorForbidden, "access_denied"
	case 9:
		category, safeCode = sdk.ErrorRateLimited, "flood_control"
	case 10, 32:
		category, safeCode = sdk.ErrorUnavailable, "remote_unavailable"
	case 14:
		category, safeCode = sdk.ErrorForbidden, "challenge_required"
	case 100:
		category, safeCode = sdk.ErrorInvalidRequest, "invalid_parameters"
	case 222:
		category, safeCode = sdk.ErrorInvalidRequest, "content_rejected"
	case 223:
		category, safeCode = sdk.ErrorRateLimited, "reply_rate_limited"
	}
	return newRemote(category, safeCode, requestID, retryAfterMS)
}

func newRemote(category sdk.ErrorCategory, code, requestID string, retryAfterMS int64) error {
	retry := time.Duration(0)
	if category == sdk.ErrorRateLimited || category == sdk.ErrorUnavailable || category == sdk.ErrorTransient {
		if retryAfterMS > 0 {
			retry = time.Duration(retryAfterMS) * time.Millisecond
		} else if category == sdk.ErrorRateLimited {
			retry = time.Second
		}
	}
	remote, err := sdk.NewRemoteError(category, code, requestID, retry)
	if err != nil {
		fallback, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return fallback
	}
	return remote
}
