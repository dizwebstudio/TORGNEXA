package cian

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	apiHost      = "public-api.cian.ru"
	maxBodyBytes = 8 << 20
)

var ErrInvalidResponse = errors.New("cian: invalid response")

type Operation string

const (
	OperationImportState  Operation = "import.state"
	OperationImportReport Operation = "import.report"
)

type Request struct {
	Method        string
	Host          string
	Operation     Operation
	Authorization []byte
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

func Manifest() sdk.Manifest { manifest, _ := sdk.CatalogManifest("cian"); return manifest }

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
		token := bytes.TrimSpace(secret)
		if len(token) < 20 || len(token) > 8192 || len(token) != len(secret) || bytes.IndexAny(token, " \t\r\n") >= 0 {
			return remote(sdk.ErrorUnauthorized, "auth_rejected", "", 0)
		}
		return fn(cfg, token)
	})
}

func request(op Operation, token []byte) Request {
	auth := make([]byte, len("Bearer ")+len(token))
	copy(auth, "Bearer ")
	copy(auth[len("Bearer "):], token)
	return Request{Method: "GET", Host: apiHost, Operation: op, Authorization: auth}
}

func (c *Connector) Health(ctx context.Context, a sdk.Account, rt sdk.Runtime) (sdk.Health, error) {
	at := c.now().UTC()
	err := c.with(ctx, a, rt, func(cfg Configuration, token []byte) error {
		r, err := c.transport.Do(ctx, request(OperationImportState, token))
		if err != nil {
			return remote(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
		}
		if err = normalize(r); err != nil {
			return err
		}
		state, err := parseImportEvidence(r.Body)
		if err != nil || state.FeedURL != cfg.FeedURL {
			return ErrInvalidResponse
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
			status, reason = sdk.HealthDegraded, "access_denied"
		case sdk.ErrorRateLimited:
			status, reason = sdk.HealthDegraded, "rate_limited"
		}
		return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: at}, nil
	}
	return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "remote_contract_invalid", CheckedAt: at}, nil
}

func normalize(r Response) error {
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
	case 402, 403:
		cat, code = sdk.ErrorForbidden, "access_denied"
	case 404:
		cat, code = sdk.ErrorNotFound, "resource_missing"
	case 429:
		cat, code = sdk.ErrorRateLimited, "rate_limited"
	case 500, 502, 503, 504:
		cat, code = sdk.ErrorUnavailable, "remote_unavailable"
	}
	retry := time.Duration(0)
	if cat == sdk.ErrorRateLimited || cat == sdk.ErrorUnavailable || cat == sdk.ErrorTransient {
		retry = time.Duration(r.RetryAfterMS) * time.Millisecond
	}
	return remote(cat, code, r.RequestID, retry)
}

func remote(cat sdk.ErrorCategory, code, id string, retry time.Duration) error {
	r, _ := sdk.NewRemoteError(cat, code, id, retry)
	return r
}

type importEvidence struct {
	FeedURL     string
	OrderID     string
	ProcessedAt time.Time
	HasProblems bool
	Total       int64
	Inserted    int64
	Updated     int64
	Deleted     int64
	Skipped     int64
	Errors      int64
	Notices     int64
}

func parseImportEvidence(body []byte) (importEvidence, error) {
	var raw struct {
		FeedURL     string           `json:"feed_url"`
		FeedURLAlt  string           `json:"feedUrl"`
		URL         string           `json:"url"`
		OrderID     json.RawMessage  `json:"order_id"`
		OrderIDAlt  json.RawMessage  `json:"orderId"`
		OrderNumber json.RawMessage  `json:"order_number"`
		ProcessedAt string           `json:"processed_at"`
		LastProcess string           `json:"last_processing_date"`
		HasProblems bool             `json:"has_problems"`
		HasErrors   bool             `json:"has_errors"`
		Total       int64            `json:"total"`
		Inserted    int64            `json:"inserted"`
		Updated     int64            `json:"updated"`
		Deleted     int64            `json:"deleted"`
		Skipped     int64            `json:"skipped"`
		Errors      int64            `json:"errors"`
		Notices     int64            `json:"notices"`
		Result      *json.RawMessage `json:"result"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if dec.Decode(&raw) != nil {
		return importEvidence{}, ErrInvalidResponse
	}
	if raw.Result != nil && len(*raw.Result) > 0 && string(*raw.Result) != "null" {
		if nested, err := parseImportEvidence(*raw.Result); err == nil {
			return nested, nil
		}
	}
	feed := firstNonEmpty(raw.FeedURL, raw.FeedURLAlt, raw.URL)
	if !validHTTPSURL(feed, 4096) {
		return importEvidence{}, ErrInvalidResponse
	}
	order := firstScalar(raw.OrderID, raw.OrderIDAlt, raw.OrderNumber)
	if order == "" {
		return importEvidence{}, ErrInvalidResponse
	}
	processed := firstNonEmpty(raw.ProcessedAt, raw.LastProcess)
	var at time.Time
	if processed != "" {
		parsed, err := parseProviderTime(processed)
		if err != nil {
			return importEvidence{}, ErrInvalidResponse
		}
		at = parsed
	}
	vals := []int64{raw.Total, raw.Inserted, raw.Updated, raw.Deleted, raw.Skipped, raw.Errors, raw.Notices}
	for _, v := range vals {
		if v < 0 {
			return importEvidence{}, ErrInvalidResponse
		}
	}
	return importEvidence{FeedURL: feed, OrderID: order, ProcessedAt: at, HasProblems: raw.HasProblems || raw.HasErrors || raw.Errors > 0, Total: raw.Total, Inserted: raw.Inserted, Updated: raw.Updated, Deleted: raw.Deleted, Skipped: raw.Skipped, Errors: raw.Errors, Notices: raw.Notices}, nil
}

func firstScalar(values ...json.RawMessage) string {
	for _, raw := range values {
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) == nil && validRemoteID(s) {
			return s
		}
		var n json.Number
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if dec.Decode(&n) == nil && validRemoteID(n.String()) {
			return n.String()
		}
	}
	return ""
}

func validRemoteID(v string) bool {
	if v == "" || v != strings.TrimSpace(v) || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func parseProviderTime(v string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, ErrInvalidResponse
}
