package avito

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"strconv"
	"time"
)

const apiHost = "api.avito.ru"
const maxBodyBytes = 12 << 20

var ErrInvalidResponse = errors.New("avito: invalid response")

type Request struct {
	Method, Host, Path, Query string
	Body                      []byte
	Bearer                    []byte
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
	return &Connector{t, c, now}
}
func Manifest() sdk.Manifest                { manifest, _ := sdk.CatalogManifest("avito"); return manifest }
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }
func (c *Connector) with(ctx context.Context, a sdk.Account, rt sdk.Runtime, fn func(Configuration, []byte) error) error {
	if c == nil || c.transport == nil || c.config == nil || rt == nil || rt.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil {
		return ErrInvalidResponse
	}
	cfg, err := c.config.Resolve(ctx, a)
	if err != nil || cfg.Validate() != nil {
		return ErrInvalidResponse
	}
	return rt.Secrets().UseSecret(ctx, a.SecretReference, func(secret []byte) error {
		if len(secret) < 20 || len(secret) > 8192 || !bytes.Equal(secret, bytes.TrimSpace(secret)) {
			return remote(sdk.ErrorUnauthorized, "auth_rejected", "", 0)
		}
		return fn(cfg, secret)
	})
}
func (c *Connector) Health(ctx context.Context, a sdk.Account, rt sdk.Runtime) (sdk.Health, error) {
	at := c.now().UTC()
	err := c.with(ctx, a, rt, func(cfg Configuration, tok []byte) error {
		r, e := c.transport.Do(ctx, Request{Method: "GET", Host: apiHost, Path: "/core/v1/accounts/self", Bearer: tok})
		if e != nil {
			return remote(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
		}
		if e = normalize(r, false); e != nil {
			return e
		}
		var v struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal(r.Body, &v) != nil || v.ID != cfg.UserID {
			return ErrInvalidResponse
		}
		return nil
	})
	if err == nil {
		return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: at}, nil
	}
	var re *sdk.RemoteError
	if errors.As(err, &re) {
		reason := "remote_unavailable"
		status := sdk.HealthUnavailable
		if re.Category == sdk.ErrorUnauthorized || re.Category == sdk.ErrorForbidden {
			status = sdk.HealthDegraded
			reason = "auth_rejected"
		}
		if re.Category == sdk.ErrorRateLimited {
			status = sdk.HealthDegraded
			reason = "rate_limited"
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
func pathUser(cfg Configuration, suffix string) string {
	return "/messenger/v2/accounts/" + strconv.FormatInt(cfg.UserID, 10) + suffix
}
