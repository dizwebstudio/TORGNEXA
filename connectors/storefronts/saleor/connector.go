package saleor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const maxBodyBytes = 16 << 20

var (
	ErrTransportMissing   = errors.New("saleor: transport missing")
	ErrInvalidResponse    = errors.New("saleor: invalid response")
	ErrInvalidCredentials = errors.New("saleor: invalid credentials")
)

type Request struct {
	Method string
	Host   string
	Path   string
	Body   []byte
	Bearer []byte
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

// Connector holds short-lived, in-memory-only caches for the channel and
// warehouse global ids resolved from their admin-supplied slugs. Like
// Shopware's currency-UUID cache, this connector is constructed fresh per
// registry call (Configuration varies per account), so the cache only pays
// off within one call's own sub-requests, not across separate top-level
// operations -- an accepted efficiency trade-off, not a correctness issue.
type Connector struct {
	transport Transport
	configs   ConfigurationSource
	now       func() time.Time

	mu         sync.Mutex
	channels   map[string]resolvedChannel
	warehouses map[string]string
}

func New(transport Transport, configs ConfigurationSource, now func() time.Time) *Connector {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Connector{transport: transport, configs: configs, now: now, channels: map[string]resolvedChannel{}, warehouses: map[string]string{}}
}

func Manifest() sdk.Manifest {
	return sdk.Manifest{
		ID: "saleor", Name: "Saleor", Family: sdk.FamilyStorefront, Version: "1.0.0", SDKVersion: sdk.SDKMajor,
		Capabilities: []sdk.Capability{"inventory.read", "inventory.write", "notifications.receive", "orders.read", "orders.status.write", "prices.read", "prices.write", "products.read", "products.write", "returns.read"},
		Auth:         []sdk.AuthRequirement{{Kind: sdk.AuthBearer, SecretClass: "marketplace.bearer-token", Required: true}},
		RateLimit:    sdk.RateLimitPolicy{MaxConcurrency: 4, MinIntervalMS: 100, RequestTimeoutMS: 30000, Retry: sdk.RetryPolicy{MaxAttempts: 5, BaseBackoffMS: 500, MaxBackoffMS: 30000}},
	}
}
func (connector *Connector) Manifest() sdk.Manifest { return Manifest() }

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

// credentials holds only a Saleor App's own long-lived access token (Apps &
// webhooks > <app> > token, minted once at install/creation time): unlike
// Shopware's client_credentials exchange, Saleor issues a static bearer
// token used directly on every GraphQL request -- no runtime token exchange
// or refresh is needed on this connector's side, the same shape Magento's
// Integration token already uses.
type credentials struct{ Token string }

func parseCredentials(raw []byte) (credentials, error) {
	if len(raw) < 8 || len(raw) > 4096 || !utf8.Valid(raw) || !bytes.Equal(raw, bytes.TrimSpace(raw)) {
		return credentials{}, ErrInvalidCredentials
	}
	value := string(raw)
	for _, r := range value {
		if r <= 0x20 || r == 0x7f {
			return credentials{}, ErrInvalidCredentials
		}
	}
	return credentials{Token: value}, nil
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
		defer func() { value.Token = "" }()
		return callback(value)
	})
}

type graphqlErrorEntry struct {
	Message    string `json:"message"`
	Extensions struct {
		Exception struct {
			Code string `json:"code"`
		} `json:"exception"`
	} `json:"extensions"`
}

type graphqlEnvelope struct {
	Data   json.RawMessage     `json:"data"`
	Errors []graphqlErrorEntry `json:"errors"`
}

// graphql executes one GraphQL operation and returns its "data" field.
// Saleor's GraphQL view returns HTTP 200 for almost every outcome including
// authentication/permission failures -- it only uses 400 for a query the
// server refused to execute at all (parse/validation/query-cost failure)
// -- so, unlike this repository's REST connectors, the HTTP status code
// alone cannot distinguish success from an auth or permission error; the
// top-level "errors[].extensions.exception.code" field must be inspected
// instead. That field is set by Saleor's own error formatter to the Python
// exception class name (saleor/graphql/utils/__init__.py format_error),
// verified against Saleor's own published core source.
func (connector *Connector) graphql(ctx context.Context, configuration Configuration, credential credentials, query string, variables map[string]any) (json.RawMessage, error) {
	if connector == nil || connector.transport == nil {
		return nil, ErrTransportMissing
	}
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return nil, ErrInvalidResponse
	}
	token := []byte(credential.Token)
	defer clear(token)
	response, err := connector.transport.Do(ctx, Request{Method: "POST", Host: configuration.StoreHost, Path: configuration.graphqlPath(), Body: body, Bearer: token})
	if err != nil {
		return nil, normalizedTransportError()
	}
	if len(response.Body) > maxBodyBytes || response.RetryAfterMS < 0 {
		remote, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return nil, remote
	}
	if response.StatusCode != 200 && response.StatusCode != 400 {
		return nil, normalizeTransportStatus(response)
	}
	var envelope graphqlEnvelope
	if json.Unmarshal(response.Body, &envelope) != nil {
		return nil, ErrInvalidResponse
	}
	if len(envelope.Errors) > 0 {
		return nil, classifyGraphQLError(envelope.Errors[0].Extensions.Exception.Code, response.RequestID, response.RetryAfterMS)
	}
	if response.StatusCode != 200 || len(envelope.Data) == 0 {
		return nil, ErrInvalidResponse
	}
	return envelope.Data, nil
}

// classifyGraphQLError maps Saleor's exception-class-name error codes to the
// Connector SDK's normalized error taxonomy. "PermissionDenied" is raised
// both for an unauthenticated request and for one authenticated as an App
// lacking a required permission scope; every other handled exception in
// Saleor's own HANDLED_EXCEPTIONS tuple that mentions a token/signature
// indicates the bearer credential itself was rejected outright.
func classifyGraphQLError(code, requestID string, retryAfterMS int64) error {
	switch {
	case code == "PermissionDenied":
		remote, _ := sdk.NewRemoteError(sdk.ErrorForbidden, "access_denied", requestID, 0)
		return remote
	case strings.Contains(code, "JWT") || strings.Contains(code, "Token") || strings.Contains(code, "Signature") || strings.Contains(code, "Decode"):
		remote, _ := sdk.NewRemoteError(sdk.ErrorUnauthorized, "auth_rejected", requestID, 0)
		return remote
	case code == "GraphQLError" || code == "ValidationError":
		remote, _ := sdk.NewRemoteError(sdk.ErrorInvalidRequest, "request_rejected", requestID, 0)
		return remote
	default:
		retry := time.Duration(retryAfterMS) * time.Millisecond
		remote, err := sdk.NewRemoteError(sdk.ErrorTransient, "remote_error", requestID, retry)
		if err != nil {
			remote, _ = sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		}
		return remote
	}
}

// mutationErrorEntry mirrors the field/message/code shape every Saleor
// mutation payload uses for its own domain-level "errors" list (ProductError,
// OrderError, ...), distinct from the top-level GraphQL "errors" array
// handled in graphql() above.
type mutationErrorEntry struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

func mutationErr(entries []mutationErrorEntry) error {
	if len(entries) == 0 {
		return nil
	}
	if entries[0].Code == "NOT_FOUND" {
		remote, _ := sdk.NewRemoteError(sdk.ErrorNotFound, "resource_missing", "", 0)
		return remote
	}
	remote, _ := sdk.NewRemoteError(sdk.ErrorInvalidRequest, "request_rejected", "", 0)
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
func unsupportedOperation(code string) error {
	remote, _ := sdk.NewRemoteError(sdk.ErrorUnsupported, code, "", 0)
	return remote
}
func newNotFound() error {
	remote, _ := sdk.NewRemoteError(sdk.ErrorNotFound, "resource_missing", "", 0)
	return remote
}
func isAmbiguousWrite(err error) bool {
	if err == nil {
		return false
	}
	var remote *sdk.RemoteError
	if errors.As(err, &remote) {
		return remote.Category == sdk.ErrorUnavailable || remote.Category == sdk.ErrorTimeout || remote.Category == sdk.ErrorTransient
	}
	return false
}

// normalizeTransportStatus handles HTTP-layer failures that never reach
// Saleor's own GraphQL error envelope at all: a fronting gateway/CDN
// rejecting the request outright (429, 5xx) or a network-adjacent proxy
// error, the same status-driven mapping every REST connector in this
// repository already applies.
func normalizeTransportStatus(response Response) error {
	category, code := sdk.ErrorTransient, "remote_error"
	switch response.StatusCode {
	case 401:
		category, code = sdk.ErrorUnauthorized, "auth_rejected"
	case 403:
		category, code = sdk.ErrorForbidden, "access_denied"
	case 404:
		category, code = sdk.ErrorNotFound, "resource_missing"
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
		_, callErr := connector.listVariants(ctx, configuration, credential, 1, "")
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

func validRemoteText(value string, max int) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > max {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
