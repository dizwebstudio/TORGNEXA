package connectorrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOAuthMigrationStoresDigestOnlyAndConsumesOnce(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000066_connector_oauth_sessions.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sqlText := strings.ToUpper(string(data))
	for _, required := range []string{
		"STATE_SHA256", "PENDING_SECRET_REFERENCE", "ACCOUNT_VERSION", "ACTOR_ID", "CALLBACK_URL",
		"EXPIRES_AT<=CREATED_AT+INTERVAL '10 MINUTES'", "STATUS IN ('PENDING','CONSUMED')",
		"ENABLE ROW LEVEL SECURITY", "FORCE ROW LEVEL SECURITY", "CURRENT_SETTING('APP.ORGANIZATION_ID',TRUE)",
		"CURRENT_SETTING('APP.WORKSPACE_ID',TRUE)", "REVOKE DELETE,TRUNCATE", "OLD.STATUS<>''PENDING''",
		"NEW.STATUS<>''CONSUMED''", "OAUTH_STATE", "RAW OAUTH STATE IS FORBIDDEN",
	} {
		if !strings.Contains(sqlText, required) {
			t.Errorf("migration missing %q", required)
		}
	}
	for _, forbidden := range []string{"CODE_VERIFIER TEXT", "ACCESS_TOKEN TEXT", "REFRESH_TOKEN TEXT", "CLIENT_SECRET TEXT", "RAW_STATE TEXT"} {
		if strings.Contains(sqlText, forbidden) {
			t.Errorf("migration contains secret-bearing column %q", forbidden)
		}
	}
}
