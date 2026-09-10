package api

import (
	"os"
	"strings"
	"testing"
)

func TestWebhookOpenAPIIncludesVerifiedRetryResponses(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/openapi/torgnexa-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paymentWebhooksPathPrefix, commerceWebhooksPathPrefix, socialWebhooksPathPrefix} {
		start := strings.Index(string(raw), "  "+strings.TrimPrefix(path, "/api/v1")+"{connector_id}")
		if start < 0 {
			t.Fatalf("missing public webhook contract for %s", path)
		}
		operation := string(raw)[start:]
		if next := strings.Index(operation[3:], "\n  /"); next >= 0 {
			operation = operation[:next+3]
		}
		for _, required := range []string{"security: []", "'200':", "'503':", "Retry-After:", "additionalProperties: false"} {
			if !strings.Contains(operation, required) {
				t.Errorf("%s missing %s", path, required)
			}
		}
	}
}
