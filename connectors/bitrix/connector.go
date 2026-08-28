package bitrix

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
	ErrTransportMissing   = errors.New("bitrix: transport missing")
	ErrInvalidResponse    = errors.New("bitrix: invalid response")
	ErrInvalidCredentials = errors.New("bitrix: invalid credentials")
	ErrProductNotFound    = errors.New("bitrix: product not found")
)

// Request is the host-mediated REST call. UserID and WebhookCode are secret
// path components and are consumed only by the reviewed builtin transport.
type Request struct {
	Method      string
	Host        string
	BasePath    string
	APIMethod   string
	Body        []byte
	UserID      []byte
	WebhookCode []byte
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
		ID: "bitrix", Name: "1С-Битрикс", Family: sdk.FamilyStorefront, Version: "1.0.0", SDKVersion: sdk.SDKMajor,
		Capabilities: []sdk.Capability{"inventory.read", "inventory.write", "orders.read", "orders.status.write", "prices.read", "prices.write", "products.read", "products.write"},
		Auth:         []sdk.AuthRequirement{{Kind: sdk.AuthBearer, SecretClass: "storefront.bridge-token", Required: true}},
		RateLimit:    sdk.RateLimitPolicy{MaxConcurrency: 4, MinIntervalMS: 150, RequestTimeoutMS: 30000, Retry: sdk.RetryPolicy{MaxAttempts: 4, BaseBackoffMS: 500, MaxBackoffMS: 30000}},
	}
}

func (connector *Connector) Manifest() sdk.Manifest { return Manifest() }

type credentials struct {
	UserID      string `json:"user_id"`
	WebhookCode string `json:"webhook_code"`
}

func validPathPart(value string, min, max int, digitsOnly bool) bool {
	if len(value) < min || len(value) > max || value != strings.TrimSpace(value) {
		return false
	}
	for _, r := range value {
		if r <= 0x20 || r == 0x7f || (digitsOnly && (r < '0' || r > '9')) || (!digitsOnly && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && !strings.ContainsRune("._~-", r)) {
			return false
		}
	}
	return true
}

func parseCredentials(raw []byte) (credentials, error) {
	if len(raw) > 8192 || !utf8.Valid(raw) || !bytes.Equal(raw, bytes.TrimSpace(raw)) {
		return credentials{}, ErrInvalidCredentials
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value credentials
	if decoder.Decode(&value) != nil {
		return credentials{}, ErrInvalidCredentials
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF || !validPathPart(value.UserID, 1, 32, true) || !validPathPart(value.WebhookCode, 8, 512, false) {
		return credentials{}, ErrInvalidCredentials
	}
	return value, nil
}

func (connector *Connector) configuration(ctx context.Context, account sdk.Account) (Configuration, error) {
	if connector == nil || connector.configs == nil {
		return Configuration{}, ErrConfigurationMissing
	}
	configuration, err := connector.configs.Resolve(ctx, account)
	if err != nil || configuration.Validate() != nil {
		return Configuration{}, ErrInvalidConfiguration
	}
	return configuration, nil
}

func (connector *Connector) withCredentials(ctx context.Context, runtime sdk.Runtime, reference sdk.SecretReference, callback func(credentials) error) error {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || callback == nil {
		return ErrInvalidCredentials
	}
	return runtime.Secrets().UseSecret(ctx, reference, func(secret []byte) error {
		value, err := parseCredentials(secret)
		if err != nil {
			return err
		}
		defer func() { value.UserID, value.WebhookCode = "", "" }()
		return callback(value)
	})
}

func (connector *Connector) call(ctx context.Context, configuration Configuration, credential credentials, apiMethod string, body any) (Response, error) {
	if connector == nil || connector.transport == nil {
		return Response{}, ErrTransportMissing
	}
	payload, err := json.Marshal(body)
	if err != nil || len(payload) > maxBodyBytes {
		return Response{}, ErrInvalidResponse
	}
	userID, webhookCode := []byte(credential.UserID), []byte(credential.WebhookCode)
	defer clear(userID)
	defer clear(webhookCode)
	response, err := connector.transport.Do(ctx, Request{Method: "POST", Host: configuration.StoreHost, BasePath: configuration.apiBasePath(), APIMethod: apiMethod, Body: payload, UserID: userID, WebhookCode: webhookCode})
	if err != nil {
		return Response{}, normalizedTransportError()
	}
	if err := normalizeHTTP(response); err != nil {
		return response, err
	}
	if err := normalizeAPIError(response); err != nil {
		return response, err
	}
	return response, nil
}

func normalizeAPIError(response Response) error {
	var envelope struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(response.Body, &envelope) != nil {
		return ErrInvalidResponse
	}
	if envelope.Error == "" {
		return nil
	}
	category, code := sdk.ErrorInvalidRequest, "request_rejected"
	upper := strings.ToUpper(envelope.Error)
	switch {
	case strings.Contains(upper, "AUTH"), strings.Contains(upper, "TOKEN"):
		category, code = sdk.ErrorUnauthorized, "auth_rejected"
	case strings.Contains(upper, "ACCESS"), strings.Contains(upper, "PERMISSION"):
		category, code = sdk.ErrorForbidden, "access_denied"
	case strings.Contains(upper, "LIMIT"), strings.Contains(upper, "TOO_MANY"):
		category, code = sdk.ErrorRateLimited, "rate_limited"
	case strings.Contains(upper, "NOT_FOUND"):
		category, code = sdk.ErrorNotFound, "resource_missing"
	}
	remote, _ := sdk.NewRemoteError(category, code, response.RequestID, time.Duration(response.RetryAfterMS)*time.Millisecond)
	return remote
}

func normalizeHTTP(response Response) error {
	if len(response.Body) > maxBodyBytes || response.RetryAfterMS < 0 {
		return ErrInvalidResponse
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
	remote, _ := sdk.NewRemoteError(category, code, response.RequestID, time.Duration(response.RetryAfterMS)*time.Millisecond)
	return remote
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

func (connector *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	at := connector.now().UTC()
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "configuration_invalid", CheckedAt: at}, nil
	}
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		response, callErr := connector.call(ctx, configuration, credential, "catalog.product.list", map[string]any{"select": []string{"id"}, "filter": map[string]any{"iblockId": configuration.CatalogIblockID}, "order": map[string]string{"id": "asc"}, "start": 0})
		if callErr != nil {
			return callErr
		}
		_, err := decodeProductList(response.Body)
		return err
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
