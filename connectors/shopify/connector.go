package shopify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// apiVersion is a fixed, pinned Shopify Admin REST API release
// (https://shopify.dev/docs/api/usage/versioning). Bumping it is a
// deliberate, reviewed change, not something a merchant's runtime config
// can influence.
const apiVersion = "2025-01"

const maxBodyBytes = 16 << 20

var (
	ErrTransportMissing   = errors.New("shopify: transport missing")
	ErrInvalidResponse    = errors.New("shopify: invalid response")
	ErrInvalidCredentials = errors.New("shopify: invalid credentials")
)

type QueryParam struct{ Name, Value string }

type Request struct {
	Method string
	Host   string
	Path   string
	Query  []QueryParam
	Body   []byte
	Bearer []byte
}

// Response.NextPageInfo carries Shopify's opaque cursor extracted from the
// Link response header's rel="next" entry, empty when there is no next page.
type Response struct {
	StatusCode   int
	Body         []byte
	RequestID    string
	RetryAfterMS int64
	NextPageInfo string
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
		ID: "shopify", Name: "Shopify", Family: sdk.FamilyStorefront, Version: "1.0.0", SDKVersion: sdk.SDKMajor,
		Capabilities: []sdk.Capability{"inventory.read", "inventory.write", "notifications.receive", "orders.read", "orders.status.write", "prices.read", "prices.write", "products.read", "products.write", "returns.read"},
		Auth: []sdk.AuthRequirement{{Kind: sdk.AuthOAuth2, SecretClass: "marketplace.oauth-token", Required: true, OAuth2: &sdk.OAuth2Configuration{
			GrantType: "authorization_code", AuthorizationURL: "https://{host}/admin/oauth/authorize", TokenURL: "https://{host}/admin/oauth/access_token",
			Scopes: []string{"read_products", "write_products", "read_inventory", "write_inventory", "read_orders", "write_orders"}, ClientAuthMethod: "client_secret_post",
			ScopeSeparator: ",", HostParameter: "shop_domain", HostSuffix: ".myshopify.com", ExtraTokenParams: map[string]string{"expiring": "1"},
		}}},
		RateLimit: sdk.RateLimitPolicy{MaxConcurrency: 2, MinIntervalMS: 500, RequestTimeoutMS: 30000, Retry: sdk.RetryPolicy{MaxAttempts: 5, BaseBackoffMS: 500, MaxBackoffMS: 30000}},
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

// credentials holds only the current access token: the host-owned OAuth
// refresh runtime (Task 134) projects the encrypted authorization-code
// bundle down to this one callback-scoped value before a provider adapter
// ever sees it, so this connector never sees the refresh token or the app's
// client secret.
type credentials struct{ AccessToken string }

func parseCredentials(raw []byte) (credentials, error) {
	if len(raw) < 16 || len(raw) > 8192 || !utf8.Valid(raw) || !bytes.Equal(raw, bytes.TrimSpace(raw)) {
		return credentials{}, ErrInvalidCredentials
	}
	value := string(raw)
	for _, r := range value {
		if r <= 0x20 || r == 0x7f {
			return credentials{}, ErrInvalidCredentials
		}
	}
	return credentials{AccessToken: value}, nil
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
		defer func() { value.AccessToken = "" }()
		return callback(value)
	})
}

func (connector *Connector) call(ctx context.Context, configuration Configuration, credential credentials, method, path string, query []QueryParam, body []byte) (Response, error) {
	if connector == nil || connector.transport == nil {
		return Response{}, ErrTransportMissing
	}
	token := []byte(credential.AccessToken)
	defer clear(token)
	response, err := connector.transport.Do(ctx, Request{Method: method, Host: configuration.ShopDomain, Path: configuration.apiPath(path), Query: query, Body: body, Bearer: token})
	if err != nil {
		return Response{}, normalizedTransportError()
	}
	if remote := normalizeHTTP(response); remote != nil {
		return response, remote
	}
	return response, nil
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
		response, callErr := connector.call(ctx, configuration, credential, "GET", "/shop.json", []QueryParam{{Name: "fields", Value: "id"}}, nil)
		if callErr != nil {
			return callErr
		}
		var value struct {
			Shop struct {
				ID int64 `json:"id"`
			} `json:"shop"`
		}
		if json.Unmarshal(response.Body, &value) != nil || value.Shop.ID < 1 {
			return ErrInvalidResponse
		}
		return nil
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
func unsupportedOperation(code string) error {
	remote, _ := sdk.NewRemoteError(sdk.ErrorUnsupported, code, "", 0)
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

func parsePositiveID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		return 0, ErrInvalidResponse
	}
	return id, nil
}

func intString(value int64) string { return strconv.FormatInt(value, 10) }

// variantRemoteID/parseVariantRemoteID: unlike WooCommerce's product-vs-
// variation duality, every Shopify variant (including a simple product's
// single default variant) already has its own directly addressable id, so
// the remote id is just that id with a type tag for readability.
func variantRemoteID(variantID int64) string { return "variant:" + intString(variantID) }

func parseVariantRemoteID(value string) (int64, error) {
	rest, ok := strings.CutPrefix(value, "variant:")
	if !ok {
		return 0, ErrInvalidResponse
	}
	return parsePositiveID(rest)
}
