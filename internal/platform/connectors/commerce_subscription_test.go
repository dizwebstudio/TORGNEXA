package connectors

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestCommerceSubscriptionBindingContract(t *testing.T) {
	ref := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	digest := sha256.Sum256([]byte(ref))
	binding := CommerceWebhookSubscription{ReferenceSHA256: hex.EncodeToString(digest[:]), Topic: "order.updated"}
	config := func(items ...CommerceWebhookSubscription) json.RawMessage {
		raw, _ := json.Marshal(map[string]any{"commerce_webhook_subscriptions": items})
		return raw
	}
	topic, err := ExpectedCommerceWebhookTopic(config(binding), ref)
	if err != nil || topic != "order.updated" {
		t.Fatal("valid binding rejected", err)
	}
	for _, raw := range []json.RawMessage{config(), config(binding, binding), config(CommerceWebhookSubscription{ReferenceSHA256: binding.ReferenceSHA256, Topic: "ORDER_UPDATED"}), config(CommerceWebhookSubscription{ReferenceSHA256: "short", Topic: binding.Topic}), json.RawMessage(`{"commerce_webhook_subscriptions":"invalid"}`), json.RawMessage(`{"commerce_webhook_subscriptions":null}`)} {
		if _, err := ExpectedCommerceWebhookTopic(raw, ref); err == nil {
			t.Fatal("invalid binding accepted")
		}
	}
	for _, bad := range []string{"", ref + "=", strings.Repeat("B", 43), strings.Repeat("A", 42)} {
		if _, err := ExpectedCommerceWebhookTopic(config(binding), bad); err == nil {
			t.Fatal("invalid reference accepted")
		}
	}
}
