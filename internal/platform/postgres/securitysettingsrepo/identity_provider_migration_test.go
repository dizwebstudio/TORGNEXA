package securitysettingsrepo

import (
	"os"
	"strings"
	"testing"
)

func TestIdentityProviderSettingsMigrationIsVersionedAndTenantScoped(t *testing.T) {
	raw, err := os.ReadFile("../../../../migrations_legacy_pre_v1/000065_identity_provider_settings.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(string(raw))
	for _, required := range []string{"SETTINGS_IDENTITY_PROVIDERS", "SETTINGS_IDENTITY_PROVIDER_REVISIONS", "SETTINGS_IDENTITY_PROVIDER_VALIDATIONS", "FORCE ROW LEVEL SECURITY", "CLIENT_SECRET_REFERENCE", "SECRET_REFERENCES", "DEFERRABLE INITIALLY DEFERRED", "REJECT_EVIDENCE_MUTATION", "REVOKE UPDATE,DELETE,TRUNCATE"} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration missing %q", required)
		}
	}
	for _, forbidden := range []string{" CLIENT_SECRET TEXT", " ACCESS_TOKEN ", " REFRESH_TOKEN ", " PASSWORD "} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("migration contains plaintext secret field %q", forbidden)
		}
	}
}
