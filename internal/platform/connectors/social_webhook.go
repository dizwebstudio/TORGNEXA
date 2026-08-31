package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

var ErrInvalidSocialWebhook = errors.New("connectors: invalid social webhook")

var socialWebhookEventPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)
var socialWebhookDeliveryPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var hexFingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// SocialWebhookRequest is an ephemeral inbound-provider webhook envelope.
// VerificationToken is provider-supplied verification material from the HTTP
// request (for MAX this is X-Max-Bot-Api-Secret). Callers must not persist or
// log it. Body is bounded provider payload and is treated as untrusted data.
type SocialWebhookRequest struct {
	VerificationToken []byte    `json:"-"`
	Body              []byte    `json:"-"`
	ReceivedAt        time.Time `json:"received_at"`
}

func (request SocialWebhookRequest) Validate() error {
	if len(request.VerificationToken) < 1 || len(request.VerificationToken) > 4096 || len(request.Body) < 2 || len(request.Body) > 2<<20 || !json.Valid(request.Body) || request.ReceivedAt.IsZero() || request.ReceivedAt.Location() != time.UTC {
		return ErrInvalidSocialWebhook
	}
	return nil
}

// SocialWebhookResult is the provider-normalized identity of an inbound
// delivery. CanonicalPayload is deterministic JSON suitable for the Task-009
// inbox/event boundary; it remains untrusted provider data.
type SocialWebhookResult struct {
	DeliveryID       string          `json:"delivery_id"`
	EventType        string          `json:"event_type"`
	RemoteChannelID  string          `json:"remote_channel_id"`
	RemoteObjectID   string          `json:"remote_object_id,omitempty"`
	OccurredAt       time.Time       `json:"occurred_at"`
	Duplicate        bool            `json:"duplicate"`
	CanonicalPayload json.RawMessage `json:"canonical_payload"`
}

func (result SocialWebhookResult) Validate() error {
	if !socialWebhookDeliveryPattern.MatchString(result.DeliveryID) || !socialWebhookEventPattern.MatchString(result.EventType) || !validRemoteReadID(result.RemoteChannelID) || result.OccurredAt.IsZero() || result.OccurredAt.Location() != time.UTC || len(result.CanonicalPayload) < 2 || len(result.CanonicalPayload) > 2<<20 || !json.Valid(result.CanonicalPayload) {
		return ErrInvalidSocialWebhook
	}
	if result.RemoteObjectID != "" && !validRemoteReadID(result.RemoteObjectID) {
		return ErrInvalidSocialWebhook
	}
	return nil
}

// SocialWebhookClaim is the minimized verified delivery handed to the host's
// durable replay boundary. CanonicalPayload is available only while the claim
// is being processed; the host must not persist it when it contains untrusted
// provider data.
type SocialWebhookClaim struct {
	DeliveryID          string
	EventType           string
	RemoteChannelID     string
	RemoteObjectID      string
	OccurredAt          time.Time
	ProviderFingerprint string
	CanonicalPayload    json.RawMessage
}

// Validate checks the bounded normalized fields of a verified webhook claim.
func (claim SocialWebhookClaim) Validate() error {
	if !socialWebhookDeliveryPattern.MatchString(claim.DeliveryID) || !socialWebhookEventPattern.MatchString(claim.EventType) || !validRemoteReadID(claim.RemoteChannelID) || (claim.RemoteObjectID != "" && !validRemoteReadID(claim.RemoteObjectID)) || claim.OccurredAt.IsZero() || claim.OccurredAt.Location() != time.UTC || !hexFingerprintPattern.MatchString(claim.ProviderFingerprint) || len(claim.CanonicalPayload) < 2 || len(claim.CanonicalPayload) > 2<<20 || !json.Valid(claim.CanonicalPayload) {
		return ErrInvalidSocialWebhook
	}
	return nil
}

// SocialWebhookDeduplicator is the host-owned durable replay boundary. A
// production implementation must be tenant-scoped and atomic (Task-009 inbox
// is the intended persistence primitive). Duplicate=true is a successful no-op.
type SocialWebhookDeduplicator interface {
	ClaimSocialWebhook(context.Context, Account, SocialWebhookClaim) (duplicate bool, err error)
}

// SocialWebhookReceiver is additive SDK-v1 surface for providers that support
// verified inbound webhook events. It does not modify frozen Connector/Runtime.
type SocialWebhookReceiver interface {
	// VerificationHeader returns the provider header that carries the
	// callback-scoped verification token. The host reads this header but does
	// not contain provider-specific header dispatch.
	VerificationHeader() string
	ReceiveSocialWebhook(context.Context, Account, Runtime, SocialWebhookRequest, SocialWebhookDeduplicator) (SocialWebhookResult, error)
}

// SocialWebhookController is the provider-neutral lifecycle surface for an
// outgoing webhook subscription. Implementations must keep credentials inside
// the runtime callback and must not return provider payloads.
type SocialWebhookController interface {
	SubscribeSocialWebhook(context.Context, Account, Runtime, string) error
	UnsubscribeSocialWebhook(context.Context, Account, Runtime, string) error
}
