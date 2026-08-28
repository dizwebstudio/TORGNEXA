package medusa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const maxBodyBytes = 16 << 20

var (
	ErrTransportMissing   = errors.New("medusa: transport missing")
	ErrInvalidResponse    = errors.New("medusa: invalid response")
	ErrInvalidCredentials = errors.New("medusa: invalid credentials")
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
		ID: "medusa", Name: "Medusa", Family: sdk.FamilyStorefront, Version: "1.0.0", SDKVersion: sdk.SDKMajor,
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

// credentials holds only the secret admin API key: Medusa is self-hosted, so
// unlike Shopify there is no host-owned OAuth runtime involved — a single
// opaque secret key created in the Medusa admin dashboard is used as-is.
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

func (connector *Connector) call(ctx context.Context, configuration Configuration, credential credentials, method, path string, query []QueryParam, body []byte) (Response, error) {
	if connector == nil || connector.transport == nil {
		return Response{}, ErrTransportMissing
	}
	token := []byte(credential.Token)
	defer clear(token)
	response, err := connector.transport.Do(ctx, Request{Method: method, Host: configuration.StoreHost, Path: configuration.apiPath(path), Query: query, Body: body, Bearer: token})
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
		response, callErr := connector.call(ctx, configuration, credential, "GET", "/products", []QueryParam{{Name: "limit", Value: "1"}, {Name: "fields", Value: "id"}}, nil)
		if callErr != nil {
			return callErr
		}
		var value struct {
			Products []struct {
				ID string `json:"id"`
			} `json:"products"`
		}
		if json.Unmarshal(response.Body, &value) != nil || len(value.Products) > 1 {
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

// variantRemoteID/parseVariantRemoteID embed both product and variant id:
// Medusa's variant sub-resource routes always require the parent product id
// too (/admin/products/{product_id}/variants/{variant_id}), so the remote id
// this connector hands back must carry both, the same compound-target
// pattern WooCommerce uses for its product/variation duality.
func variantRemoteID(productID, variantID string) string {
	return "variant:" + productID + ":" + variantID
}

func parseVariantRemoteID(value string) (productID, variantID string, err error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 || parts[0] != "variant" || parts[1] == "" || parts[2] == "" {
		return "", "", ErrInvalidResponse
	}
	return parts[1], parts[2], nil
}
