package autoru

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	apiHost      = "apiauto.ru"
	apiBasePath  = "/1.0"
	maxBodyBytes = 12 << 20
)

var ErrInvalidResponse = errors.New("auto-ru: invalid response")

type Request struct {
	Method        string
	Host          string
	Path          string
	Query         string
	Body          []byte
	Authorization []byte
	SessionID     []byte
	DealerID      string
	ContentType   string
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
	config    ConfigurationSource
	now       func() time.Time
}

func New(t Transport, c ConfigurationSource, now func() time.Time) *Connector {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Connector{transport: t, config: c, now: now}
}

func Manifest() sdk.Manifest { manifest, _ := sdk.CatalogManifest("auto-ru"); return manifest }

func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

type credentials struct {
	Authorization string `json:"authorization"`
	SessionID     string `json:"session_id,omitempty"`
}

func parseCredentials(secret []byte) (credentials, error) {
	var out credentials
	if len(secret) < 20 || len(secret) > 16384 || !bytes.Equal(secret, bytes.TrimSpace(secret)) || json.Unmarshal(secret, &out) != nil {
		return out, remote(sdk.ErrorUnauthorized, "auth_rejected", "", 0)
	}
	if !validCredentialPart(out.Authorization, 8192) || (out.SessionID != "" && !validCredentialPart(out.SessionID, 8192)) {
		return credentials{}, remote(sdk.ErrorUnauthorized, "auth_rejected", "", 0)
	}
	return out, nil
}

func validCredentialPart(v string, max int) bool {
	if v == "" || v != strings.TrimSpace(v) || len(v) > max {
		return false
	}
	for _, r := range v {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func (c *Connector) with(ctx context.Context, a sdk.Account, rt sdk.Runtime, fn func(Configuration, credentials) error) error {
	if c == nil || c.transport == nil || c.config == nil || rt == nil || rt.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil {
		return ErrInvalidResponse
	}
	cfg, err := c.config.Resolve(ctx, a)
	if err != nil || cfg.Validate() != nil {
		return ErrInvalidResponse
	}
	return rt.Secrets().UseSecret(ctx, a.SecretReference, func(secret []byte) error {
		creds, err := parseCredentials(secret)
		if err != nil {
			return err
		}
		return fn(cfg, creds)
	})
}

func request(method, path, query string, body []byte, cfg Configuration, creds credentials) Request {
	return Request{
		Method:        method,
		Host:          apiHost,
		Path:          apiBasePath + path,
		Query:         query,
		Body:          body,
		Authorization: []byte(creds.Authorization),
		SessionID:     []byte(creds.SessionID),
		DealerID:      cfg.DealerID,
		ContentType:   "application/json",
	}
}

func (c *Connector) Health(ctx context.Context, a sdk.Account, rt sdk.Runtime) (sdk.Health, error) {
	at := c.now().UTC()
	err := c.with(ctx, a, rt, func(cfg Configuration, creds credentials) error {
		r, err := c.transport.Do(ctx, request("GET", "/dealer/account", "", nil, cfg, creds))
		if err != nil {
			return remote(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
		}
		if err = normalize(r, false); err != nil {
			return err
		}
		var v struct {
			AccountID    json.RawMessage `json:"account_id"`
			DealerStatus string          `json:"dealer_status"`
		}
		dec := json.NewDecoder(bytes.NewReader(r.Body))
		dec.UseNumber()
		if dec.Decode(&v) != nil || v.DealerStatus == "" {
			return ErrInvalidResponse
		}
		idText, ok := scalarID(v.AccountID)
		if !ok {
			return ErrInvalidResponse
		}
		id, err := strconv.ParseInt(idText, 10, 64)
		if err != nil || id != cfg.AccountID {
			return ErrInvalidResponse
		}
		if strings.ToUpper(v.DealerStatus) != "ACTIVE" {
			return remote(sdk.ErrorForbidden, "account_inactive", r.RequestID, 0)
		}
		return nil
	})
	if err == nil {
		return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: at}, nil
	}
	var re *sdk.RemoteError
	if errors.As(err, &re) {
		status, reason := sdk.HealthUnavailable, "remote_unavailable"
		switch re.Category {
		case sdk.ErrorUnauthorized:
			status, reason = sdk.HealthDegraded, "auth_rejected"
		case sdk.ErrorForbidden:
			status, reason = sdk.HealthDegraded, re.Code
		case sdk.ErrorRateLimited:
			status, reason = sdk.HealthDegraded, "rate_limited"
		}
		return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: at}, nil
	}
	return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "remote_contract_invalid", CheckedAt: at}, nil
}

func normalize(r Response, write bool) error {
	if len(r.Body) > maxBodyBytes || r.RetryAfterMS < 0 {
		return remote(sdk.ErrorInternal, "response_invalid", "", 0)
	}
	if r.StatusCode >= 200 && r.StatusCode < 300 {
		return nil
	}
	cat, code := sdk.ErrorTransient, "remote_error"
	switch r.StatusCode {
	case 400, 405, 406, 415, 422:
		cat, code = sdk.ErrorInvalidRequest, "request_rejected"
	case 401:
		cat, code = sdk.ErrorUnauthorized, "auth_rejected"
	case 402:
		cat, code = sdk.ErrorForbidden, "subscription_required"
	case 403:
		cat, code = sdk.ErrorForbidden, "access_denied"
	case 404:
		cat, code = sdk.ErrorNotFound, "resource_missing"
	case 409:
		cat, code = sdk.ErrorConflict, "remote_conflict"
	case 429:
		cat, code = sdk.ErrorRateLimited, "rate_limited"
	case 500, 502, 503, 504:
		cat, code = sdk.ErrorUnavailable, "remote_unavailable"
	}
	retry := time.Duration(0)
	if !write && (cat == sdk.ErrorRateLimited || cat == sdk.ErrorUnavailable || cat == sdk.ErrorTransient) {
		retry = time.Duration(r.RetryAfterMS) * time.Millisecond
	}
	if write && (cat == sdk.ErrorUnavailable || cat == sdk.ErrorTransient) {
		cat, code = sdk.ErrorConflict, "write_outcome_unknown"
	}
	return remote(cat, code, r.RequestID, retry)
}

func remote(cat sdk.ErrorCategory, code, id string, retry time.Duration) error {
	r, _ := sdk.NewRemoteError(cat, code, id, retry)
	return r
}
