package youtube

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrTransportMissing   = errors.New("youtube: transport missing")
	ErrInvalidResponse    = errors.New("youtube: invalid response")
	ErrInvalidCredentials = errors.New("youtube: invalid credentials")
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
	FailureUnavailable    FailureKind = "unavailable"
	FailureUnknownWrite   FailureKind = "unknown_write"
	FailureExpiredSession FailureKind = "expired_session"
)

type GoogleFailure struct {
	Kind       FailureKind
	Reason     string
	RequestID  string
	RetryAfter time.Duration
}

func (e *GoogleFailure) Error() string { return "youtube data api operation failed" }

type Channel struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type UploadMetadata struct {
	ExternalID              string
	Title                   string
	Description             string
	CategoryID              string
	PrivacyStatus           string
	NotifySubscribers       bool
	SelfDeclaredMadeForKids bool
	ContainsSyntheticMedia  bool
	MediaType               string
	SizeBytes               int64
	ContentSHA256           string
}

type UploadSession struct {
	ID string `json:"id"`
}

type UploadChunkRequest struct {
	SessionID  string
	Offset     int64
	TotalBytes int64
	MediaType  string
	Body       []byte
}

type UploadProgress struct {
	NextOffset int64
	Complete   bool
	Video      VideoRecord
}

type VideoRecord struct {
	VideoID          string `json:"video_id"`
	ChannelID        string `json:"channel_id"`
	UploadStatus     string `json:"upload_status"`
	ProcessingStatus string `json:"processing_status"`
	FailureReason    string `json:"failure_reason,omitempty"`
	RejectionReason  string `json:"rejection_reason,omitempty"`
}

type CommentRecord struct {
	CommentID       string    `json:"comment_id"`
	AuthorChannelID string    `json:"author_channel_id"`
	Text            string    `json:"text"`
	PublishedAt     time.Time `json:"published_at"`
}

type CommentPage struct {
	Items         []CommentRecord `json:"items"`
	NextPageToken string          `json:"next_page_token,omitempty"`
}

// Transport is an endpoint-aware host binding for the documented YouTube Data
// API v3 operations. A production implementation owns the resumable Location
// URI and exposes only an opaque SessionID to this provider, preventing the
// session URI from leaking into Core, logs or normalized errors.
type Transport interface {
	ResolveOwnedChannel(context.Context, []byte) (Channel, error)
	StartResumableUpload(context.Context, []byte, string, UploadMetadata) (UploadSession, error)
	UploadChunk(context.Context, []byte, UploadChunkRequest) (UploadProgress, error)
	ProbeResumableUpload(context.Context, []byte, string, int64) (UploadProgress, error)
	ReadVideo(context.Context, []byte, string) (VideoRecord, error)
	ListCommentThreads(context.Context, []byte, string, string, int) (CommentPage, error)
}

type Connector struct {
	transport Transport
	configs   ConfigurationSource
	now       func() time.Time
	chunkSize int
}

func New(transport Transport, configs ConfigurationSource, now func() time.Time) *Connector {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Connector{transport: transport, configs: configs, now: now, chunkSize: 8 << 20}
}

func Manifest() sdk.Manifest                { manifest, _ := sdk.CatalogManifest("youtube"); return manifest }
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

func validToken(v []byte) bool {
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

func (c *Connector) withToken(ctx context.Context, runtime sdk.Runtime, ref sdk.SecretReference, cb func([]byte) error) error {
	if c == nil || runtime == nil || runtime.Secrets() == nil || cb == nil {
		return ErrInvalidCredentials
	}
	return runtime.Secrets().UseSecret(ctx, ref, func(secret []byte) error {
		if !validToken(secret) {
			return ErrInvalidCredentials
		}
		return cb(secret)
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
	err = c.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		channel, callErr := c.transport.ResolveOwnedChannel(ctx, token)
		if callErr != nil {
			return normalizeFailure(callErr, false)
		}
		if channel.ID != cfg.ChannelID || !safeID(channel.ID, 3, 128) || !safeText(channel.Title, 1, 300) {
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
			status, reason = sdk.HealthDegraded, "youtube_permission_missing"
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
	var gf *GoogleFailure
	if !errors.As(err, &gf) || gf == nil || !validFailure(gf) {
		if write {
			return newRemote(sdk.ErrorConflict, "write_outcome_unknown", "", 0)
		}
		return newRemote(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
	}
	if write && gf.Kind == FailureUnknownWrite {
		return newRemote(sdk.ErrorConflict, "write_outcome_unknown", gf.RequestID, 0)
	}
	cat, code := sdk.ErrorTransient, "remote_error"
	switch gf.Kind {
	case FailureInvalidRequest:
		cat, code = sdk.ErrorInvalidRequest, mapGoogleReason(gf.Reason, "request_rejected")
	case FailureUnauthorized:
		cat, code = sdk.ErrorUnauthorized, "auth_rejected"
	case FailureForbidden:
		cat, code = sdk.ErrorForbidden, mapGoogleReason(gf.Reason, "access_denied")
	case FailureNotFound:
		cat, code = sdk.ErrorNotFound, "resource_missing"
	case FailureConflict:
		cat, code = sdk.ErrorConflict, "remote_conflict"
	case FailureRateLimited:
		cat, code = sdk.ErrorRateLimited, "rate_limited"
	case FailureQuotaExceeded:
		cat, code = sdk.ErrorRateLimited, mapGoogleReason(gf.Reason, "quota_exceeded")
	case FailureExpiredSession:
		cat, code = sdk.ErrorConflict, "upload_session_expired"
	case FailureUnavailable:
		if write {
			return newRemote(sdk.ErrorConflict, "write_outcome_unknown", gf.RequestID, 0)
		}
		cat, code = sdk.ErrorUnavailable, "remote_unavailable"
	case FailureUnknownWrite:
		if write {
			return newRemote(sdk.ErrorConflict, "write_outcome_unknown", gf.RequestID, 0)
		}
		cat, code = sdk.ErrorUnavailable, "remote_unavailable"
	default:
		if write {
			return newRemote(sdk.ErrorConflict, "write_outcome_unknown", gf.RequestID, 0)
		}
		cat, code = sdk.ErrorUnavailable, "remote_unavailable"
	}
	return newRemote(cat, code, gf.RequestID, gf.RetryAfter)
}

func validFailure(gf *GoogleFailure) bool {
	if gf == nil || gf.RetryAfter < 0 || gf.RetryAfter > 24*time.Hour || len(gf.RequestID) > 256 || !safeReason(gf.Reason) {
		return false
	}
	switch gf.Kind {
	case FailureInvalidRequest, FailureUnauthorized, FailureForbidden, FailureNotFound, FailureConflict, FailureRateLimited, FailureQuotaExceeded, FailureUnavailable, FailureUnknownWrite, FailureExpiredSession:
		return true
	default:
		return false
	}
}

func mapGoogleReason(reason, fallback string) string {
	switch reason {
	case "uploadLimitExceeded":
		return "upload_limit_exceeded"
	case "quotaExceeded", "dailyLimitExceeded", "dailyLimitExceededUnreg":
		return "quota_exceeded"
	case "commentsDisabled":
		return "comments_disabled"
	case "forbiddenPrivacySetting":
		return "privacy_setting_forbidden"
	case "invalidCategoryId":
		return "invalid_category"
	case "invalidDescription":
		return "invalid_description"
	case "invalidTitle":
		return "invalid_title"
	case "videoNotFound":
		return "video_not_found"
	case "channelNotFound":
		return "channel_not_found"
	case "":
		return fallback
	default:
		return fallback
	}
}

func safeReason(v string) bool {
	if v == "" {
		return true
	}
	if len(v) > 128 || v != strings.TrimSpace(v) {
		return false
	}
	for _, r := range v {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
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

func newRemote(cat sdk.ErrorCategory, code, requestID string, retry time.Duration) error {
	if retry < 0 || retry > 24*time.Hour || (cat != sdk.ErrorRateLimited && cat != sdk.ErrorUnavailable && cat != sdk.ErrorTransient) {
		retry = 0
	}
	e, err := sdk.NewRemoteError(cat, code, requestID, retry)
	if err != nil {
		fallback, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return fallback
	}
	return e
}
