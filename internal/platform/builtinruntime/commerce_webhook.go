package builtinruntime

import (
	"net/http"
	"strings"
)

// CommerceWebhookHeaders extracts the provider-owned signature and topic
// headers at the reviewed composition boundary. The application only receives
// the normalized values and does not dispatch on connector identity.
func CommerceWebhookHeaders(connectorID string, headers http.Header) (signature, topic string, ok bool) {
	var rawTopic string
	switch connectorID {
	case "saleor":
		signature = strings.TrimSpace(headers.Get("Saleor-Signature"))
		rawTopic = headers.Get("Saleor-Event")
	case "woocommerce":
		signature = strings.TrimSpace(headers.Get("X-WC-Webhook-Signature"))
		rawTopic = headers.Get("X-WC-Webhook-Topic")
	default:
		return "", "", false
	}
	topic = normalizeCommerceWebhookTopic(rawTopic)
	return signature, topic, signature != "" && topic != ""
}

func normalizeCommerceWebhookTopic(value string) string {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "_", "."))
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return ""
	}
	validResource := map[string]struct{}{"order": {}, "product": {}, "coupon": {}, "customer": {}}
	validAction := map[string]struct{}{"created": {}, "updated": {}, "deleted": {}}
	if _, ok := validResource[parts[0]]; !ok {
		return ""
	}
	if _, ok := validAction[parts[1]]; !ok {
		return ""
	}
	return parts[0] + "." + parts[1]
}
