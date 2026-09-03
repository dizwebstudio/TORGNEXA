package builtinruntime

import (
	"net/http"
	"testing"
)

func TestCommerceWebhookHeadersAdmitProviderHeaderSets(t *testing.T) {
	tests := []struct {
		name      string
		connector string
		signature string
		topic     string
		wantTopic string
	}{
		{name: "saleor", connector: "saleor", signature: "saleor-signature", topic: "PRODUCT_UPDATED", wantTopic: "product.updated"},
		{name: "woocommerce", connector: "woocommerce", signature: "woocommerce-signature", topic: "order.created", wantTopic: "order.created"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			headers := http.Header{}
			if test.connector == "saleor" {
				headers.Set("Saleor-Signature", test.signature)
				headers.Set("Saleor-Event", test.topic)
			} else {
				headers.Set("X-WC-Webhook-Signature", test.signature)
				headers.Set("X-WC-Webhook-Topic", test.topic)
			}
			signature, topic, ok := CommerceWebhookHeaders(test.connector, headers)
			if !ok || signature != test.signature || topic != test.wantTopic {
				t.Fatalf("got signature=%q topic=%q ok=%v", signature, topic, ok)
			}
		})
	}
}

func TestCommerceWebhookHeadersRejectUnknownOrIncompleteSets(t *testing.T) {
	if _, _, ok := CommerceWebhookHeaders("unknown", http.Header{}); ok {
		t.Fatal("unknown connector header set was admitted")
	}
	headers := http.Header{}
	headers.Set("Saleor-Signature", "signature")
	if _, _, ok := CommerceWebhookHeaders("saleor", headers); ok {
		t.Fatal("incomplete provider header set was admitted")
	}
}
