package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

var ErrInvalidCommerceWebhook = errors.New("connectors: invalid commerce webhook")
var commerceWebhookTopicPattern = regexp.MustCompile(`^(?:order|product|coupon|customer)\.(?:created|updated|deleted)$`)
var commerceWebhookDeliveryPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type CommerceWebhookRequest struct {
	Signature     string    `json:"-"`
	HeaderTopic   string    `json:"header_topic"`
	ExpectedTopic string    `json:"expected_topic"`
	Body          []byte    `json:"-"`
	ReceivedAt    time.Time `json:"received_at"`
}

func (request CommerceWebhookRequest) Validate() error {
	if len(request.Signature) < 16 || len(request.Signature) > 256 || !commerceWebhookTopicPattern.MatchString(request.HeaderTopic) || request.HeaderTopic != request.ExpectedTopic || len(request.Body) < 2 || len(request.Body) > 4<<20 || !json.Valid(request.Body) || request.ReceivedAt.IsZero() || request.ReceivedAt.Location() != time.UTC {
		return ErrInvalidCommerceWebhook
	}
	return nil
}

type CommerceWebhookResult struct {
	DeliveryID       string          `json:"delivery_id"`
	EventType        string          `json:"event_type"`
	ResourceKind     string          `json:"resource_kind"`
	ResourceRemoteID string          `json:"resource_remote_id"`
	OccurredAt       time.Time       `json:"occurred_at"`
	Duplicate        bool            `json:"duplicate"`
	CanonicalPayload json.RawMessage `json:"canonical_payload"`
}

func (result CommerceWebhookResult) Validate() error {
	if !commerceWebhookDeliveryPattern.MatchString(result.DeliveryID) || !commerceWebhookTopicPattern.MatchString(result.EventType) || !validNotificationKind(result.ResourceKind) || !validRemoteID(result.ResourceRemoteID) || result.OccurredAt.IsZero() || result.OccurredAt.Location() != time.UTC || len(result.CanonicalPayload) < 2 || len(result.CanonicalPayload) > 4<<20 || !json.Valid(result.CanonicalPayload) {
		return ErrInvalidCommerceWebhook
	}
	return nil
}

type CommerceWebhookDeduplicator interface {
	ClaimCommerceWebhook(context.Context, Account, string, string, time.Time) (duplicate bool, err error)
}

type CommerceWebhookReceiver interface {
	ReceiveCommerceWebhook(context.Context, Account, Runtime, CommerceWebhookRequest, CommerceWebhookDeduplicator) (CommerceWebhookResult, error)
}
