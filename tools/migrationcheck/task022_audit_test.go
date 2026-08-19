package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTask022NotificationBoundary(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	checks := map[string][]string{
		"internal/platform/notifications/notifications.go":          {"type Provider interface", "ChannelWebUI", "ChannelWebhook", "DefaultPreference", "OccurrenceCount", "WebhookProvider", "platform.notifications.notification_created.v1"},
		"internal/platform/postgres/notificationrepo/repository.go": {"set_config('app.organization_id'", "set_config('app.workspace_id'", "FOR UPDATE", "occurrence_count=occurrence_count+1", "notification_deliveries"},
		"migrations_legacy_pre_v1/000021_notifications.sql":         {"notifications_dedupe_uniq", "notifications_guard_update", "notification severity cannot decrease", "notification identity is immutable", "FORCE ROW LEVEL SECURITY", "notification_deliveries_reject_mutation", "REVOKE UPDATE ON notification_deliveries", "attempt integer NOT NULL", "PRIMARY KEY (notification_id,channel,occurrence,attempt)", "attempt BETWEEN 1 AND 64", "INSERT INTO migration_history"},
		"internal/app/api/notifications.go":                         {"resolver.NotificationIdentity(r)", "DisallowUnknownFields", "recipient_id", "Cache-Control", "no-store"},
		"contracts/notifications/notification-v1.schema.json":       {"dependentRequired", "source_event_id", "source_event_type", "entity_type", "entity_id"},
		"contracts/notifications/delivery-page-v1.schema.json":      {"allOf", "error_code", "const", "failed", "attempt", "maximum"},
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

func TestTask022DoesNotImplementSecondWebhookTransport(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	data, err := os.ReadFile(filepath.Join(root, "internal/platform/notifications/notifications.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, bad := range []string{"http.Client", "net.Dial", "Authorization", "response_body", "raw_error"} {
		if strings.Contains(text, bad) {
			t.Fatalf("notification core contains duplicate egress/persistence token %q", bad)
		}
	}
}
