package connectors

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
)

// CommerceWebhookSubscription binds a non-secret reference digest to one exact topic.
// The random reference is shared only with the remote provider's callback URL.
type CommerceWebhookSubscription struct {
	ReferenceSHA256 string `json:"reference_sha256"`
	Topic           string `json:"topic"`
}

// CommerceWebhookSubscriptions validates host-owned subscription bindings in runtime config.
func CommerceWebhookSubscriptions(raw json.RawMessage) ([]CommerceWebhookSubscription, error) {
	var config struct {
		Subscriptions json.RawMessage `json:"commerce_webhook_subscriptions"`
	}
	if len(raw) > 32768 || json.Unmarshal(raw, &config) != nil {
		return nil, ErrInvalidCommerceWebhook
	}
	if len(config.Subscriptions) == 0 {
		return nil, nil
	}
	var subscriptions []CommerceWebhookSubscription
	decoder := json.NewDecoder(bytes.NewReader(config.Subscriptions))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&subscriptions) != nil || subscriptions == nil || len(subscriptions) > 32 {
		return nil, ErrInvalidCommerceWebhook
	}
	seen := make(map[string]bool)
	for _, subscription := range subscriptions {
		digest, err := hex.DecodeString(subscription.ReferenceSHA256)
		if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != subscription.ReferenceSHA256 || seen[subscription.ReferenceSHA256] || !commerceWebhookTopicPattern.MatchString(subscription.Topic) {
			return nil, ErrInvalidCommerceWebhook
		}
		seen[subscription.ReferenceSHA256] = true
	}
	return subscriptions, nil
}

// ExpectedCommerceWebhookTopic resolves a 256-bit reference against trusted account config.
func ExpectedCommerceWebhookTopic(raw json.RawMessage, reference string) (string, error) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(reference)
	if err != nil || len(decoded) != 32 || len(reference) != 43 {
		return "", ErrInvalidCommerceWebhook
	}
	subscriptions, err := CommerceWebhookSubscriptions(raw)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(reference))
	encoded := hex.EncodeToString(digest[:])
	for _, subscription := range subscriptions {
		if subscription.ReferenceSHA256 == encoded {
			return subscription.Topic, nil
		}
	}
	return "", ErrInvalidCommerceWebhook
}
