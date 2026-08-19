package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTask063DurableWebhookSecurityBoundary(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	checks := map[string][]string{
		"internal/platform/webhooks/webhooks.go":               {"hmac.New", "VerifySignature", "DefaultReplayWindow", "EndpointPolicy", "publicIP", "CheckRedirect", "SecretProvider", "OutcomeDLQ"},
		"internal/platform/postgres/webhookrepo/repository.go": {"FOR UPDATE SKIP LOCKED", "lease_token", "ON CONFLICT (organization_id,workspace_id,subscription_id,event_id)", "webhook_delivery_attempts", "set_config('app.organization_id'", "set_config('app.workspace_id'"},
		"migrations_legacy_pre_v1/000020_durable_webhooks.sql": {"FORCE ROW LEVEL SECURITY", "webhook_deliveries_guard_update", "webhook_attempts_reject_mutation", "previous_signing_secret_reference", "INSERT INTO migration_history"},
		"internal/app/api/webhooks.go":                         {"resolver.WebhookScope(r)", "DisallowUnknownFields", "Cache-Control", "no-store"},
	}
	for rel, wants := range checks {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s missing %q", rel, want)
			}
		}
	}
}

func TestTask063DoesNotPersistWebhookSecretsOrRemoteBodies(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	for _, rel := range []string{"migrations_legacy_pre_v1/000020_durable_webhooks.sql", "internal/platform/postgres/webhookrepo/repository.go"} {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(data))
		for _, bad := range []string{"plaintext_signing_secret", "response_body text", "raw_error text", "authorization_header"} {
			if strings.Contains(lower, bad) {
				t.Fatalf("%s contains forbidden persistence %q", rel, bad)
			}
		}
	}
}
