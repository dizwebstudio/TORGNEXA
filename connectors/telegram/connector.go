package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	apiHost      = "api.telegram.org"
	maxBodyBytes = 8 << 20
)

var (
	ErrTransportMissing   = errors.New("telegram: transport missing")
	ErrInvalidResponse    = errors.New("telegram: invalid response")
	ErrInvalidCredentials = errors.New("telegram: invalid credentials")
	botTokenPattern       = regexp.MustCompile(`^[0-9]{5,20}:[A-Za-z0-9_-]{30,128}$`)
)

type Param struct {
	Name  string
	Value string
}

type FilePart struct {
	FieldName string
	FileName  string
	MediaType string
	SizeBytes int64
	SHA256    string
	Body      io.Reader
}

type Request struct {
	Method    string
	Host      string
	APIMethod string
	Params    []Param
	Files     []FilePart
	BotToken  []byte
}

type Response struct {
	StatusCode int
	Body       []byte
	RequestID  string
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
		ID: "telegram", Name: "Telegram", Family: sdk.FamilySocial, Version: "1.0.0", SDKVersion: sdk.SDKMajor,
		Capabilities: []sdk.Capability{"social.post.buttons", "social.post.delete", "social.post.edit", "social.post.media", "social.post.text", "social.post.video"},
		Auth:         []sdk.AuthRequirement{{Kind: sdk.AuthBearer, SecretClass: "social.bot-token", Required: true}},
		RateLimit: sdk.RateLimitPolicy{MaxConcurrency: 1, MinIntervalMS: 40, RequestTimeoutMS: 30000,
			Retry: sdk.RetryPolicy{MaxAttempts: 5, BaseBackoffMS: 500, MaxBackoffMS: 60000}},
	}
}

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

func validBotToken(value []byte) bool {
	return len(value) <= 160 && utf8.Valid(value) && bytes.Equal(value, bytes.TrimSpace(value)) && botTokenPattern.Match(value)
}

func (connector *Connector) withToken(ctx context.Context, runtime sdk.Runtime, reference sdk.SecretReference, callback func([]byte) error) error {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || callback == nil {
		return ErrInvalidCredentials
	}
	return runtime.Secrets().UseSecret(ctx, reference, func(secret []byte) error {
		if !validBotToken(secret) {
			return ErrInvalidCredentials
		}
		return callback(secret)
	})
}

func (connector *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if connector == nil || connector.transport == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidAccount
	}
	checkedAt := connector.now().UTC()
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "configuration_invalid", CheckedAt: checkedAt}, nil
	}
	var health sdk.Health
	err = connector.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		raw, callErr := connector.call(ctx, token, "getMe", nil, nil, false)
		if callErr != nil {
			return callErr
		}
		var me struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		}
		if json.Unmarshal(raw, &me) != nil || me.ID < 1 || !me.IsBot {
			return ErrInvalidResponse
		}
		raw, callErr = connector.call(ctx, token, "getChatMember", []Param{{Name: "chat_id", Value: strconv.FormatInt(configuration.ChatID, 10)}, {Name: "user_id", Value: strconv.FormatInt(me.ID, 10)}}, nil, false)
		if callErr != nil {
			return callErr
		}
		var member struct {
			Status          string `json:"status"`
			CanPostMessages bool   `json:"can_post_messages"`
		}
		if json.Unmarshal(raw, &member) != nil {
			return ErrInvalidResponse
		}
		if member.Status != "administrator" || !member.CanPostMessages {
			health = sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "channel_post_permission_missing", CheckedAt: checkedAt}
			return nil
		}
		health = sdk.Health{Status: sdk.HealthHealthy, CheckedAt: checkedAt}
		return nil
	})
	if err == nil {
		return health, nil
	}
	var remote *sdk.RemoteError
	if errors.As(err, &remote) {
		status, reason := sdk.HealthUnavailable, "remote_unavailable"
		if remote.Category == sdk.ErrorUnauthorized {
			status, reason = sdk.HealthDegraded, "auth_rejected"
		}
		if remote.Category == sdk.ErrorForbidden {
			status, reason = sdk.HealthDegraded, "channel_access_denied"
		}
		if remote.Category == sdk.ErrorRateLimited {
			status, reason = sdk.HealthDegraded, "rate_limited"
		}
		return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: checkedAt}, nil
	}
	if errors.Is(err, ErrInvalidResponse) || errors.Is(err, ErrInvalidCredentials) {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "remote_contract_invalid", CheckedAt: checkedAt}, nil
	}
	return sdk.Health{}, err
}

type envelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  *struct {
		RetryAfter int64 `json:"retry_after"`
	} `json:"parameters"`
}

func (connector *Connector) call(ctx context.Context, token []byte, method string, params []Param, files []FilePart, write bool) (json.RawMessage, error) {
	if connector == nil || connector.transport == nil || method == "" || !validBotToken(token) {
		return nil, ErrTransportMissing
	}
	response, err := connector.transport.Do(ctx, Request{Method: "POST", Host: apiHost, APIMethod: method, Params: append([]Param(nil), params...), Files: files, BotToken: token})
	if err != nil {
		return nil, transportError(write)
	}
	if len(response.Body) == 0 || len(response.Body) > maxBodyBytes {
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, normalizeHTTP(response, write)
		}
		return nil, ErrInvalidResponse
	}
	var parsed envelope
	parsedOK := json.Unmarshal(response.Body, &parsed) == nil
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// A write-side 5xx leaves the remote outcome ambiguous even when Telegram
		// returns a structured error body; do not make it retryable automatically.
		if write && response.StatusCode >= 500 {
			return nil, normalizeHTTP(response, true)
		}
		if parsedOK && !parsed.OK && parsed.ErrorCode != 0 {
			retry := int64(0)
			if parsed.Parameters != nil {
				retry = parsed.Parameters.RetryAfter
			}
			return nil, normalizeTelegramError(parsed.ErrorCode, response.RequestID, retry)
		}
		return nil, normalizeHTTP(response, write)
	}
	if !parsedOK {
		return nil, ErrInvalidResponse
	}
	if !parsed.OK {
		retry := int64(0)
		if parsed.Parameters != nil {
			retry = parsed.Parameters.RetryAfter
		}
		return nil, normalizeTelegramError(parsed.ErrorCode, response.RequestID, retry)
	}
	if len(parsed.Result) == 0 || bytes.Equal(parsed.Result, []byte("null")) {
		return nil, ErrInvalidResponse
	}
	return append(json.RawMessage(nil), parsed.Result...), nil
}

func transportError(write bool) error {
	category, code := sdk.ErrorUnavailable, "transport_unavailable"
	if write {
		category, code = sdk.ErrorInternal, "write_outcome_unknown"
	}
	remote, _ := sdk.NewRemoteError(category, code, "", 0)
	return remote
}

func normalizeHTTP(response Response, write bool) error {
	if len(response.Body) > maxBodyBytes {
		r, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return r
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
	case 429:
		category, code = sdk.ErrorRateLimited, "rate_limited"
	case 500, 502, 503, 504:
		if write {
			category, code = sdk.ErrorInternal, "write_outcome_unknown"
		} else {
			category, code = sdk.ErrorUnavailable, "remote_unavailable"
		}
	}
	r, _ := sdk.NewRemoteError(category, code, response.RequestID, 0)
	return r
}

func normalizeTelegramError(code int, requestID string, retryAfterSeconds int64) error {
	category, safeCode := sdk.ErrorInvalidRequest, "request_rejected"
	switch code {
	case 401:
		category, safeCode = sdk.ErrorUnauthorized, "auth_rejected"
	case 403:
		category, safeCode = sdk.ErrorForbidden, "access_denied"
	case 404:
		category, safeCode = sdk.ErrorNotFound, "resource_missing"
	case 409:
		category, safeCode = sdk.ErrorConflict, "remote_conflict"
	case 429:
		category, safeCode = sdk.ErrorRateLimited, "flood_control"
	case 500, 502, 503, 504:
		category, safeCode = sdk.ErrorUnavailable, "remote_unavailable"
	}
	retry := time.Duration(0)
	if category == sdk.ErrorRateLimited && retryAfterSeconds > 0 && retryAfterSeconds <= 86400 {
		retry = time.Duration(retryAfterSeconds) * time.Second
	}
	r, err := sdk.NewRemoteError(category, safeCode, requestID, retry)
	if err != nil {
		fallback, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return fallback
	}
	return r
}
