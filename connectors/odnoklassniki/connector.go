package ok

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	apiHost      = "api.ok.ru"
	maxBodyBytes = 12 << 20
)

var (
	ErrTransportMissing   = errors.New("ok: transport missing")
	ErrInvalidResponse    = errors.New("ok: invalid response")
	ErrInvalidCredentials = errors.New("ok: invalid credentials")
)

type Param struct{ Name, Value string }
type Request struct {
	Method, Host, APIMethod string
	Params                  []Param
	AccessToken             []byte
}
type FilePart struct {
	FieldName, FileName, MediaType, SHA256 string
	SizeBytes                              int64
	Body                                   io.Reader
}
type UploadRequest struct {
	URL   string
	Files []FilePart
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

func Manifest() sdk.Manifest                { manifest, _ := sdk.CatalogManifest("odnoklassniki"); return manifest }
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

func (c *Connector) configuration(ctx context.Context, account sdk.Account) (Configuration, error) {
	if c == nil || c.configs == nil {
		return Configuration{}, ErrConfigurationMissing
	}
	v, err := c.configs.Resolve(ctx, account)
	if err != nil || v.Validate() != nil {
		return Configuration{}, ErrInvalidConfiguration
	}
	return v, nil
}

func validSecret(v []byte, max int) bool {
	if len(v) < 16 || len(v) > max || !utf8.Valid(v) || !bytes.Equal(v, bytes.TrimSpace(v)) {
		return false
	}
	for _, b := range v {
		if b <= 0x20 || b == 0x7f {
			return false
		}
	}
	return true
}

func (c *Connector) useSecret(ctx context.Context, runtime sdk.Runtime, ref sdk.SecretReference, max int, cb func([]byte) error) error {
	if c == nil || runtime == nil || runtime.Secrets() == nil || ref == "" || cb == nil {
		return ErrInvalidCredentials
	}
	return runtime.Secrets().UseSecret(ctx, ref, func(secret []byte) error {
		if !validSecret(secret, max) {
			return ErrInvalidCredentials
		}
		return cb(secret)
	})
}

func (c *Connector) withCredentials(ctx context.Context, runtime sdk.Runtime, account sdk.Account, cfg Configuration, cb func([]byte, []byte) error) error {
	return c.useSecret(ctx, runtime, account.SecretReference, 8192, func(token []byte) error {
		return c.useSecret(ctx, runtime, cfg.AppSecretReference, 512, func(appSecret []byte) error { return cb(token, appSecret) })
	})
}

func (c *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	at := c.now().UTC()
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "configuration_invalid", CheckedAt: at}, nil
	}
	err = c.withCredentials(ctx, runtime, account, cfg, func(token, appSecret []byte) error {
		raw, e := c.call(ctx, token, appSecret, cfg.ApplicationKey, "GET", "group.getInfo", []Param{{Name: "uids", Value: cfg.GroupID}, {Name: "fields", Value: "uid,name"}}, false)
		if e != nil {
			return e
		}
		var groups []struct {
			UID json.RawMessage `json:"uid"`
			ID  json.RawMessage `json:"id"`
		}
		if json.Unmarshal(raw, &groups) != nil || len(groups) != 1 {
			return ErrInvalidResponse
		}
		id := rawID(groups[0].UID)
		if id == "" {
			id = rawID(groups[0].ID)
		}
		if id != cfg.GroupID {
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
		switch remote.Category {
		case sdk.ErrorUnauthorized:
			status, reason = sdk.HealthDegraded, "auth_rejected"
		case sdk.ErrorForbidden:
			status, reason = sdk.HealthDegraded, "group_content_permission_missing"
		case sdk.ErrorRateLimited:
			status, reason = sdk.HealthDegraded, "rate_limited"
		}
		return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: at}, nil
	}
	if errors.Is(err, ErrInvalidResponse) || errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrInvalidConfiguration) {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "remote_contract_invalid", CheckedAt: at}, nil
	}
	return sdk.Health{}, err
}

func rawID(v json.RawMessage) string {
	if len(v) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(v, &n) == nil {
		return n.String()
	}
	return ""
}

type okError struct {
	ErrorCode int `json:"error_code"`
}

func (c *Connector) call(ctx context.Context, token, appSecret []byte, applicationKey, methodHTTP, apiMethod string, params []Param, write bool) (json.RawMessage, error) {
	if c == nil || c.transport == nil || !validSecret(token, 8192) || !validSecret(appSecret, 512) || !safePublicKey(applicationKey) || (methodHTTP != "GET" && methodHTTP != "POST") || !safeMethod(apiMethod) {
		return nil, ErrTransportMissing
	}
	base := append([]Param(nil), params...)
	base = append(base, Param{Name: "application_key", Value: applicationKey}, Param{Name: "format", Value: "json"})
	sig, err := sign(base, token, appSecret)
	if err != nil {
		return nil, err
	}
	base = append(base, Param{Name: "sig", Value: sig})
	resp, err := c.transport.Do(ctx, Request{Method: methodHTTP, Host: apiHost, APIMethod: apiMethod, Params: base, AccessToken: append([]byte(nil), token...)})
	if err != nil {
		return nil, transportError(write)
	}
	if len(resp.Body) > maxBodyBytes || resp.RetryAfterMS < 0 {
		return nil, newRemote(sdk.ErrorInternal, "response_invalid", "", 0)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, normalizeHTTP(resp, write)
	}
	var oe okError
	if len(resp.Body) == 0 {
		if apiMethod == "video.update" {
			return json.RawMessage("null"), nil
		}
		return nil, ErrInvalidResponse
	}
	if json.Unmarshal(resp.Body, &oe) == nil && oe.ErrorCode != 0 {
		return nil, normalizeOKError(oe.ErrorCode, resp.RequestID, resp.RetryAfterMS, write)
	}
	return append(json.RawMessage(nil), resp.Body...), nil
}

func sign(params []Param, token, appSecret []byte) (string, error) {
	if !validSecret(token, 8192) || !validSecret(appSecret, 512) {
		return "", ErrInvalidCredentials
	}
	pairs := append([]Param(nil), params...)
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Name == pairs[j].Name {
			return pairs[i].Value < pairs[j].Value
		}
		return pairs[i].Name < pairs[j].Name
	})
	inner := md5.Sum(append(append([]byte(nil), token...), appSecret...))
	secretKey := []byte(hex.EncodeToString(inner[:]))
	defer clear(secretKey)
	var b strings.Builder
	for _, p := range pairs {
		if p.Name == "access_token" || p.Name == "session_key" || p.Name == "sig" || p.Name == "" {
			continue
		}
		b.WriteString(p.Name)
		b.WriteByte('=')
		b.WriteString(p.Value)
	}
	payload := append([]byte(b.String()), secretKey...)
	defer clear(payload)
	out := md5.Sum(payload)
	return hex.EncodeToString(out[:]), nil
}

func safeMethod(v string) bool {
	if len(v) < 3 || len(v) > 96 {
		return false
	}
	for _, r := range v {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.') {
			return false
		}
	}
	return true
}

func transportError(write bool) error {
	if write {
		return newRemote(sdk.ErrorConflict, "write_outcome_unknown", "", 0)
	}
	return newRemote(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
}
func normalizeHTTP(r Response, write bool) error {
	if write && r.StatusCode >= 500 {
		return newRemote(sdk.ErrorConflict, "write_outcome_unknown", r.RequestID, 0)
	}
	cat, code := sdk.ErrorTransient, "remote_error"
	switch r.StatusCode {
	case 400, 405, 406, 415, 422:
		cat, code = sdk.ErrorInvalidRequest, "request_rejected"
	case 401:
		cat, code = sdk.ErrorUnauthorized, "auth_rejected"
	case 403:
		cat, code = sdk.ErrorForbidden, "access_denied"
	case 404:
		cat, code = sdk.ErrorNotFound, "resource_missing"
	case 409, 423:
		cat, code = sdk.ErrorConflict, "remote_conflict"
	case 429:
		cat, code = sdk.ErrorRateLimited, "rate_limited"
	case 500, 502, 503, 504:
		cat, code = sdk.ErrorUnavailable, "remote_unavailable"
	}
	return newRemote(cat, code, r.RequestID, r.RetryAfterMS)
}
func normalizeOKError(code int, req string, retryMS int64, write bool) error {
	if write && (code == 1 || code == 2 || code == 9999) {
		return newRemote(sdk.ErrorConflict, "write_outcome_unknown", req, 0)
	}
	cat, safe := sdk.ErrorInvalidRequest, "request_rejected"
	switch code {
	case 1, 2, 9999:
		cat, safe = sdk.ErrorUnavailable, "remote_unavailable"
	case 8, 11:
		cat, safe = sdk.ErrorRateLimited, "rate_limited"
	case 10, 24, 50, 200, 456, 457:
		cat, safe = sdk.ErrorForbidden, "access_denied"
	case 102, 103, 104, 401, 453:
		cat, safe = sdk.ErrorUnauthorized, "auth_rejected"
	case 300, 700:
		cat, safe = sdk.ErrorNotFound, "resource_missing"
	case 454, 511, 600, 601, 607:
		cat, safe = sdk.ErrorInvalidRequest, "content_rejected"
	case 500, 501, 502, 503, 504, 505:
		cat, safe = sdk.ErrorInvalidRequest, "media_rejected"
	}
	return newRemote(cat, safe, req, retryMS)
}
func newRemote(cat sdk.ErrorCategory, code, req string, retryMS int64) error {
	d := time.Duration(0)
	if retryMS > 0 && (cat == sdk.ErrorRateLimited || cat == sdk.ErrorUnavailable || cat == sdk.ErrorTransient) {
		d = time.Duration(retryMS) * time.Millisecond
	}
	e, err := sdk.NewRemoteError(cat, code, req, d)
	if err != nil {
		f, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return f
	}
	return e
}
