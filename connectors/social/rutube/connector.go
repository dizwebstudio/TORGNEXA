package rutube

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrTransportMissing   = errors.New("rutube: partner transport missing")
	ErrInvalidResponse    = errors.New("rutube: invalid partner response")
	ErrInvalidCredentials = errors.New("rutube: invalid credentials")
)

type FailureKind string

const (
	FailureInvalidRequest FailureKind = "invalid_request"
	FailureUnauthorized   FailureKind = "unauthorized"
	FailureForbidden      FailureKind = "forbidden"
	FailureNotFound       FailureKind = "not_found"
	FailureConflict       FailureKind = "conflict"
	FailureRateLimited    FailureKind = "rate_limited"
	FailureQuotaExceeded  FailureKind = "quota_exceeded"
	FailureRejected       FailureKind = "rejected"
	FailureUnavailable    FailureKind = "unavailable"
	FailureUnknownWrite   FailureKind = "unknown_write"
)

type PartnerFailure struct {
	Kind       FailureKind
	Code       string
	RequestID  string
	RetryAfter time.Duration
}

func (e *PartnerFailure) Error() string { return "rutube partner operation failed" }

type Channel struct {
	ID   string
	Name string
}

type UploadSession struct {
	ID        string
	MaxBytes  int64
	ExpiresAt time.Time
}

type CreateUploadRequest struct {
	ChannelID     string
	ContractID    string
	ExternalID    string
	Title         string
	Description   string
	MediaType     string
	SizeBytes     int64
	ContentSHA256 string
}

type UploadRequest struct {
	SessionID     string
	ContractID    string
	FileName      string
	MediaType     string
	SizeBytes     int64
	ContentSHA256 string
	Body          io.Reader
}

type CommitUploadRequest struct {
	SessionID  string
	ChannelID  string
	ContractID string
	ExternalID string
}

type VideoState string

const (
	VideoStateProcessing VideoState = "processing"
	VideoStatePublished  VideoState = "published"
	VideoStateFailed     VideoState = "failed"
)

type VideoRecord struct {
	VideoID    string
	ChannelID  string
	State      VideoState
	ReasonCode string
}

type VideoStatusRequest struct {
	VideoID    string
	ChannelID  string
	ContractID string
}

// PartnerTransport is intentionally typed and endpoint-free. RUTUBE does not
// publish a current open upload contract. A production binding must be backed
// by the account-specific official partner contract and must not use Studio
// cookies, DOM automation or reverse-engineered private endpoints.
type PartnerTransport interface {
	ResolveChannel(context.Context, []byte, string, string) (Channel, error)
	CreateUpload(context.Context, []byte, CreateUploadRequest) (UploadSession, error)
	Upload(context.Context, []byte, UploadRequest) error
	CommitUpload(context.Context, []byte, CommitUploadRequest) (VideoRecord, error)
	ReadVideo(context.Context, []byte, VideoStatusRequest) (VideoRecord, error)
}

type Connector struct {
	transport PartnerTransport
	configs   ConfigurationSource
	now       func() time.Time
}

func New(transport PartnerTransport, configs ConfigurationSource, now func() time.Time) *Connector {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Connector{transport: transport, configs: configs, now: now}
}

func Manifest() sdk.Manifest {
	return sdk.Manifest{
		ID: "rutube", Name: "RUTUBE", Family: sdk.FamilySocial, Version: "1.0.0", SDKVersion: sdk.SDKMajor,
		Capabilities: []sdk.Capability{"social.post.video"},
		Auth:         []sdk.AuthRequirement{{Kind: sdk.AuthBearer, SecretClass: "social.partner-credential", Required: true}},
		RateLimit: sdk.RateLimitPolicy{MaxConcurrency: 1, MinIntervalMS: 1000, RequestTimeoutMS: 300000,
			Retry: sdk.RetryPolicy{MaxAttempts: 4, BaseBackoffMS: 2000, MaxBackoffMS: 60000}},
	}
}
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

func (c *Connector) configuration(ctx context.Context, account sdk.Account) (Configuration, error) {
	if c == nil || c.configs == nil {
		return Configuration{}, ErrConfigurationMissing
	}
	cfg, err := c.configs.Resolve(ctx, account)
	if err != nil || cfg.Validate() != nil {
		return Configuration{}, ErrInvalidConfiguration
	}
	return cfg, nil
}

func validCredential(v []byte) bool {
	if len(v) < 16 || len(v) > 8192 || !utf8.Valid(v) || !bytes.Equal(v, bytes.TrimSpace(v)) {
		return false
	}
	for _, b := range v {
		if b <= 0x20 || b == 0x7f {
			return false
		}
	}
	return true
}

func (c *Connector) withCredential(ctx context.Context, runtime sdk.Runtime, ref sdk.SecretReference, fn func([]byte) error) error {
	if c == nil || runtime == nil || runtime.Secrets() == nil || fn == nil {
		return ErrInvalidCredentials
	}
	return runtime.Secrets().UseSecret(ctx, ref, func(secret []byte) error {
		if !validCredential(secret) {
			return ErrInvalidCredentials
		}
		return fn(secret)
	})
}

func (c *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || runtime == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	at := c.now().UTC()
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "configuration_invalid", CheckedAt: at}, nil
	}
	err = c.withCredential(ctx, runtime, account.SecretReference, func(secret []byte) error {
		channel, callErr := c.transport.ResolveChannel(ctx, secret, cfg.ContractID, cfg.ChannelID)
		if callErr != nil {
			return normalizeFailure(callErr, false)
		}
		if channel.ID != cfg.ChannelID || !safeID(channel.ID, 1, 128) || !safeText(channel.Name, 1, 300) {
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
			status, reason = sdk.HealthDegraded, "partner_contract_forbidden"
		case sdk.ErrorRateLimited:
			status, reason = sdk.HealthDegraded, remote.Code
		case sdk.ErrorNotFound:
			status, reason = sdk.HealthDegraded, "channel_not_found"
		}
		return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: at}, nil
	}
	if errors.Is(err, ErrInvalidResponse) || errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrInvalidConfiguration) {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "remote_contract_invalid", CheckedAt: at}, nil
	}
	return sdk.Health{}, err
}

func normalizeFailure(err error, write bool) error {
	var pf *PartnerFailure
	if !errors.As(err, &pf) || pf == nil || !validFailure(pf) {
		if write {
			return newRemote(sdk.ErrorConflict, "write_outcome_unknown", "", 0)
		}
		return newRemote(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
	}
	if write && pf.Kind == FailureUnknownWrite {
		return newRemote(sdk.ErrorConflict, "write_outcome_unknown", pf.RequestID, 0)
	}
	cat, code := sdk.ErrorTransient, "remote_error"
	switch pf.Kind {
	case FailureInvalidRequest:
		cat, code = sdk.ErrorInvalidRequest, safeFailureCode(pf.Code, "request_rejected")
	case FailureUnauthorized:
		cat, code = sdk.ErrorUnauthorized, "auth_rejected"
	case FailureForbidden:
		cat, code = sdk.ErrorForbidden, "access_denied"
	case FailureNotFound:
		cat, code = sdk.ErrorNotFound, "resource_missing"
	case FailureConflict:
		cat, code = sdk.ErrorConflict, safeFailureCode(pf.Code, "remote_conflict")
	case FailureRateLimited:
		cat, code = sdk.ErrorRateLimited, "rate_limited"
	case FailureQuotaExceeded:
		cat, code = sdk.ErrorRateLimited, "quota_exceeded"
	case FailureRejected:
		cat, code = sdk.ErrorInvalidRequest, safeFailureCode(pf.Code, "content_rejected")
	case FailureUnavailable:
		if write {
			return newRemote(sdk.ErrorConflict, "write_outcome_unknown", pf.RequestID, 0)
		}
		cat, code = sdk.ErrorUnavailable, "remote_unavailable"
	case FailureUnknownWrite:
		if write {
			return newRemote(sdk.ErrorConflict, "write_outcome_unknown", pf.RequestID, 0)
		}
		cat, code = sdk.ErrorUnavailable, "remote_unavailable"
	default:
		if write {
			return newRemote(sdk.ErrorConflict, "write_outcome_unknown", pf.RequestID, 0)
		}
		cat, code = sdk.ErrorUnavailable, "remote_unavailable"
	}
	return newRemote(cat, code, pf.RequestID, pf.RetryAfter)
}

func validFailure(pf *PartnerFailure) bool {
	if pf == nil || pf.RetryAfter < 0 || pf.RetryAfter > 24*time.Hour || len(pf.RequestID) > 256 || !safeOptionalCode(pf.Code) {
		return false
	}
	switch pf.Kind {
	case FailureInvalidRequest, FailureUnauthorized, FailureForbidden, FailureNotFound, FailureConflict, FailureRateLimited, FailureQuotaExceeded, FailureRejected, FailureUnavailable, FailureUnknownWrite:
		return true
	default:
		return false
	}
}

func safeFailureCode(v, fallback string) string {
	if safeOptionalCode(v) && v != "" {
		return v
	}
	return fallback
}
func safeOptionalCode(v string) bool {
	if v == "" {
		return true
	}
	if len(v) > 64 || v != strings.TrimSpace(v) || v[0] < 'a' || v[0] > 'z' {
		return false
	}
	for _, r := range v {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}
func safeText(v string, min, max int) bool {
	if !utf8.ValidString(v) || v != strings.TrimSpace(v) {
		return false
	}
	n := utf8.RuneCountInString(v)
	if n < min || n > max {
		return false
	}
	for _, r := range v {
		if r == 0 || r == '\u007f' {
			return false
		}
	}
	return true
}
func newRemote(cat sdk.ErrorCategory, code, req string, retry time.Duration) error {
	if retry < 0 || retry > 24*time.Hour || (cat != sdk.ErrorRateLimited && cat != sdk.ErrorUnavailable && cat != sdk.ErrorTransient) {
		retry = 0
	}
	e, err := sdk.NewRemoteError(cat, code, req, retry)
	if err != nil {
		fallback, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return fallback
	}
	return e
}
