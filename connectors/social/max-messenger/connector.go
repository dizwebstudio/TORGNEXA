package maxconnector

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
	apiHost      = "platform-api2.max.ru"
	maxBodyBytes = 12 << 20
)

var (
	ErrTransportMissing   = errors.New("max: transport missing")
	ErrInvalidResponse    = errors.New("max: invalid response")
	ErrInvalidCredentials = errors.New("max: invalid credentials")
)

type Param struct{ Name, Value string }

type Request struct {
	Method      string
	Host        string
	Path        string
	Params      []Param
	Body        []byte
	AccessToken []byte
}

type UploadRequest struct {
	URL       string
	FileName  string
	MediaType string
	SizeBytes int64
	SHA256    string
	Body      io.Reader
	// AccessToken is valid only during the host transport callback. It is
	// intentionally not part of normalized media metadata or persisted state.
	AccessToken []byte
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

func Manifest() sdk.Manifest                        { manifest, _ := sdk.CatalogManifest("max-messenger"); return manifest }
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

func validWebhookSecret(value []byte) bool {
	if len(value) < 5 || len(value) > 256 || !utf8.Valid(value) || !bytes.Equal(value, bytes.TrimSpace(value)) {
		return false
	}
	for _, b := range value {
		if !((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-') {
			return false
		}
	}
	return true
}

func (connector *Connector) useSecret(ctx context.Context, runtime sdk.Runtime, reference sdk.SecretReference, validator func([]byte) bool, callback func([]byte) error) error {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || reference == "" || validator == nil || callback == nil {
		return ErrInvalidCredentials
	}
	return runtime.Secrets().UseSecret(ctx, reference, func(secret []byte) error {
		if !validator(secret) {
			return ErrInvalidCredentials
		}
		return callback(secret)
	})
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
	err = connector.useSecret(ctx, runtime, account.SecretReference, validToken, func(token []byte) error {
		raw, callErr := connector.call(ctx, token, "GET", "/me", nil, nil, false)
		if callErr != nil {
			return callErr
		}
		var me struct {
			UserID int64 `json:"user_id"`
			IsBot  bool  `json:"is_bot"`
		}
		if json.Unmarshal(raw, &me) != nil || me.UserID == 0 || !me.IsBot {
			return ErrInvalidResponse
		}
		raw, callErr = connector.call(ctx, token, "GET", "/chats/"+strconv.FormatInt(configuration.ChatID, 10), nil, nil, false)
		if callErr != nil {
			return callErr
		}
		var chat struct {
			ChatID int64  `json:"chat_id"`
			Type   string `json:"type"`
			Status string `json:"status"`
		}
		if json.Unmarshal(raw, &chat) != nil || chat.ChatID != configuration.ChatID || chat.Type != "channel" || chat.Status != "active" {
			return ErrInvalidResponse
		}
		raw, callErr = connector.call(ctx, token, "GET", "/chats/"+strconv.FormatInt(configuration.ChatID, 10)+"/members/me", nil, nil, false)
		if callErr != nil {
			return callErr
		}
		var member struct {
			UserID      int64    `json:"user_id"`
			IsBot       bool     `json:"is_bot"`
			IsAdmin     bool     `json:"is_admin"`
			IsOwner     bool     `json:"is_owner"`
			Permissions []string `json:"permissions"`
		}
		if json.Unmarshal(raw, &member) != nil || member.UserID != me.UserID || !member.IsBot || (!member.IsAdmin && !member.IsOwner) || !(hasPermission(member.Permissions, "write") || hasPermission(member.Permissions, "post_edit_delete_message")) {
			return permissionError()
		}
		return nil
	})
	if err == nil {
		return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: checkedAt}, nil
	}
	var remote *sdk.RemoteError
	if errors.As(err, &remote) {
		status, reason := sdk.HealthUnavailable, "remote_unavailable"
		switch remote.Category {
		case sdk.ErrorUnauthorized:
			status, reason = sdk.HealthDegraded, "auth_rejected"
		case sdk.ErrorForbidden:
			status, reason = sdk.HealthDegraded, "channel_post_permission_missing"
		case sdk.ErrorRateLimited:
			status, reason = sdk.HealthDegraded, "rate_limited"
		}
		return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: checkedAt}, nil
	}
	if errors.Is(err, ErrInvalidResponse) || errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrInvalidConfiguration) {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "remote_contract_invalid", CheckedAt: checkedAt}, nil
	}
	return sdk.Health{}, err
}

func hasPermission(values []string, wanted string) bool {
	for _, v := range values {
		if v == wanted {
			return true
		}
	}
	return false
}
func permissionError() error {
	e, _ := sdk.NewRemoteError(sdk.ErrorForbidden, "channel_post_permission_missing", "", 0)
	return e
}

func (connector *Connector) call(ctx context.Context, token []byte, method, path string, params []Param, body []byte, write bool) (json.RawMessage, error) {
	if connector == nil || connector.transport == nil || method == "" || path == "" || path[0] != '/' || !validToken(token) {
		return nil, ErrTransportMissing
	}
	response, err := connector.transport.Do(ctx, Request{Method: method, Host: apiHost, Path: path, Params: append([]Param(nil), params...), Body: append([]byte(nil), body...), AccessToken: token})
	if err != nil {
		return nil, transportError(write)
	}
	if remote := normalizeHTTP(response, write); remote != nil {
		return nil, remote
	}
	if len(response.Body) == 0 || len(response.Body) > maxBodyBytes || !json.Valid(response.Body) {
		return nil, ErrInvalidResponse
	}
	var problem struct {
		Code    json.RawMessage `json:"code"`
		Success *bool           `json:"success"`
	}
	_ = json.Unmarshal(response.Body, &problem)
	if problem.Success != nil && !*problem.Success {
		return nil, remoteError(sdk.ErrorInvalidRequest, "request_rejected", response.RequestID, response.RetryAfterMS)
	}
	return append(json.RawMessage(nil), response.Body...), nil
}

func transportError(write bool) error {
	if write {
		return remoteError(sdk.ErrorInternal, "write_outcome_unknown", "", 0)
	}
	return remoteError(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
}
func normalizeHTTP(r Response, write bool) error {
	if len(r.Body) > maxBodyBytes || r.RetryAfterMS < 0 {
		return remoteError(sdk.ErrorInternal, "response_invalid", "", 0)
	}
	if r.StatusCode >= 200 && r.StatusCode < 300 {
		return nil
	}
	if write && r.StatusCode >= 500 {
		return remoteError(sdk.ErrorInternal, "write_outcome_unknown", r.RequestID, 0)
	}
	category, code := sdk.ErrorTransient, "remote_error"
	switch r.StatusCode {
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
	case 429:
		category, code = sdk.ErrorRateLimited, "rate_limited"
	case 500, 502, 503, 504:
		category, code = sdk.ErrorUnavailable, "remote_unavailable"
	}
	return remoteError(category, code, r.RequestID, r.RetryAfterMS)
}
func remoteError(category sdk.ErrorCategory, code, id string, retryMS int64) error {
	retry := time.Duration(0)
	if retryMS > 0 && (category == sdk.ErrorRateLimited || category == sdk.ErrorUnavailable || category == sdk.ErrorTransient) {
		retry = time.Duration(retryMS) * time.Millisecond
	}
	if category == sdk.ErrorRateLimited && retry == 0 {
		retry = time.Second
	}
	e, err := sdk.NewRemoteError(category, code, id, retry)
	if err != nil {
		f, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return f
	}
	return e
}
