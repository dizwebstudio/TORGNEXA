package privacyrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrivacyMigrationSecurityShape(t *testing.T) {
	t.Parallel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller unavailable")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", "..", "..", "migrations_legacy_pre_v1", "000006_privacy_foundation.sql")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(data))
	required := []string{
		"create table privacy_purposes", "create table privacy_retention_policies",
		"legal_basis text", "notice_reference text", "consent_reference text", "allowed_classes jsonb",
		"retention_days integer", "disposition text", "legal_hold_permitted boolean",
		"force row level security", "privacy_purposes_tenant_select", "privacy_retention_tenant_select",
		"current_setting('app.organization_id', true)", "current_setting('app.workspace_id', true)",
		"privacy_retention_validate_purpose", "allowed_classes ? new.data_class",
		"privacy_registry_reject_delete", "retired privacy purpose cannot be reactivated",
	}
	for _, token := range required {
		if !strings.Contains(text, token) {
			t.Errorf("migration missing %q", token)
		}
	}
	for _, forbidden := range []string{"customer_email", "phone_number", "full_name", "passport_number", "subject_payload", "raw_pii"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("privacy registry contains raw subject-data column/token %q", forbidden)
		}
	}
}
