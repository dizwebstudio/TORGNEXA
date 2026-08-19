package connectorrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConnectorSDKMigrationHardensAccountModel(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000012_connector_sdk.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sqlText := strings.ToUpper(string(data))
	for _, required := range []string{
		"ADD COLUMN VERSION BIGINT", "HEALTH_STATUS TEXT", "HEALTH_REASON_CODE TEXT", "HEALTH_CHECKED_AT TIMESTAMPTZ",
		"'FX'", "'NOTIFICATION'", "REVOKE DELETE, TRUNCATE ON CONNECTOR_ACCOUNTS FROM PUBLIC",
		"CONNECTOR_ACCOUNTS_SDK_GUARD", "CONNECTOR_ACCOUNTS_NO_DELETE", "CONNECTOR_ACCOUNTS_NO_CLEAR",
		"RAW PROVIDER ERRORS/RESPONSES ARE FORBIDDEN", "PLAINTEXT CREDENTIALS ARE FORBIDDEN",
	} {
		if !strings.Contains(sqlText, required) {
			t.Errorf("migration missing %q", required)
		}
	}
	for _, forbidden := range []string{"ADD COLUMN PASSWORD", "ADD COLUMN ACCESS_TOKEN", "ADD COLUMN REFRESH_TOKEN", "ADD COLUMN CLIENT_SECRET", "ADD COLUMN API_KEY"} {
		if strings.Contains(sqlText, forbidden) {
			t.Errorf("migration contains plaintext credential shape %q", forbidden)
		}
	}
}
