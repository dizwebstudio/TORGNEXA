package securitysettingsrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSettingsSecurityMigrationSafety(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "..", "migrations_legacy_pre_v1", "000061_settings_security.sql"))
	if err != nil {
		t.Fatal(err)
	}
	value := strings.ToUpper(strings.Join(strings.Fields(string(data)), " "))
	for _, fragment := range []string{"BEGIN;", "FORCE ROW LEVEL SECURITY", "SESSION_REF ~ '^[0-9A-F]{64}$'", "SETTINGS_IDENTITY_SESSIONS_ENFORCE_TRANSITION", "OLD.STATUS=''REVOKED''", "SETTINGS_LOGIN_EVENTS_NO_UPDATE", "REVOKE UPDATE,DELETE,TRUNCATE ON SETTINGS_LOGIN_EVENTS", "RAW OIDC SID/SUB"} {
		if !strings.Contains(value, fragment) {
			t.Errorf("migration is missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"BEARER_TOKEN", "ACCESS_TOKEN", "USER_AGENT", "IP_ADDRESS", " ON DELETE CASCADE"} {
		if strings.Contains(value, forbidden) {
			t.Errorf("migration contains forbidden %q", forbidden)
		}
	}
}
