package connectors

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSocialWebhookRequestAndResultValidation(t *testing.T) {
	received := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	request := SocialWebhookRequest{VerificationToken: []byte("secret"), Body: []byte(`{"update_type":"message_created"}`), ReceivedAt: received}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	result := SocialWebhookResult{
		DeliveryID:       "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		EventType:        "max.message_created",
		RemoteChannelID:  "-70801090403050",
		RemoteObjectID:   "mid.ffffbdb48e6c3775019d496b34394b84",
		OccurredAt:       received.Add(-time.Minute),
		CanonicalPayload: json.RawMessage(`{"chat_id":-70801090403050}`),
	}
	if err := result.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSocialWebhookValidationFailsClosed(t *testing.T) {
	received := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	badRequests := []SocialWebhookRequest{
		{Body: []byte(`{}`), ReceivedAt: received},
		{VerificationToken: []byte("secret"), Body: []byte(`not-json`), ReceivedAt: received},
		{VerificationToken: []byte("secret"), Body: []byte(`{}`), ReceivedAt: received.In(time.FixedZone("x", 3600))},
	}
	for _, request := range badRequests {
		if request.Validate() == nil {
			t.Fatalf("invalid request accepted: %#v", request)
		}
	}
	result := SocialWebhookResult{DeliveryID: "provider-event-1", EventType: "max.message_created", RemoteChannelID: "-1", OccurredAt: received, CanonicalPayload: json.RawMessage(`{}`)}
	if result.Validate() == nil {
		t.Fatal("non-content-addressed delivery id accepted")
	}
}

func TestSocialWebhooksCapabilityIsSocialOnly(t *testing.T) {
	if err := socialManifest("social.webhooks").Validate(); err != nil {
		t.Fatal(err)
	}
	marketplace := Manifest{ID: "bad-webhooks", Name: "Bad", Family: FamilyMarketplace, Version: "1.0.0", SDKVersion: SDKMajor, Capabilities: []Capability{"social.webhooks"}, Auth: []AuthRequirement{{Kind: AuthBearer, SecretClass: "token", Required: true}}, RateLimit: RateLimitPolicy{MaxConcurrency: 1, MinIntervalMS: 10, RequestTimeoutMS: 1000, Retry: RetryPolicy{MaxAttempts: 2, BaseBackoffMS: 10, MaxBackoffMS: 20}}}
	if marketplace.Validate() == nil {
		t.Fatal("social.webhooks admitted outside social family")
	}
}
