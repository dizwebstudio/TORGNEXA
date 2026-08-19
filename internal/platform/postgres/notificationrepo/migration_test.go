package notificationrepo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotificationMigrationHasTenantRLSAndImmutableDeliveryHistory(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "migrations_legacy_pre_v1", "000021_notifications.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, needle := range []string{
		"CREATE TABLE notifications", "notifications_dedupe_uniq", "CREATE TABLE notification_preferences", "CREATE TABLE notification_deliveries",
		"notifications_guard_update", "notification severity cannot decrease", "notification identity is immutable", "notifications_source_event_type_chk", "FORCE ROW LEVEL SECURITY", "current_setting('app.organization_id',true)", "current_setting('app.workspace_id',true)",
		"notification_deliveries_reject_mutation", "REVOKE UPDATE ON notification_deliveries", "attempt integer NOT NULL", "PRIMARY KEY (notification_id,channel,occurrence,attempt)", "notification_deliveries_attempt_chk", "attempt BETWEEN 1 AND 64", "remote bodies, headers, tokens and raw provider errors are forbidden",
	} {
		if !strings.Contains(s, needle) {
			t.Fatalf("migration missing %q", needle)
		}
	}
}
