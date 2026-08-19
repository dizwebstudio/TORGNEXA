package privacyrepo

import (
	"strings"
	"testing"
)

func TestRepositorySQLIsTenantScopedAndLifecycleOnly(t *testing.T) {
	t.Parallel()
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) succeeded")
	}
	if !strings.Contains(applyScopeStatement, "true)") || !strings.Contains(applyScopeStatement, "app.organization_id") || !strings.Contains(applyScopeStatement, "app.workspace_id") {
		t.Fatalf("tenant scope SQL is not transaction-local: %q", applyScopeStatement)
	}
	for name, statement := range map[string]string{
		"create purpose":   insertPurposeStatement,
		"update purpose":   updatePurposeStatement,
		"create retention": insertRetentionStatement,
		"update retention": updateRetentionStatement,
	} {
		upper := " " + strings.ToUpper(strings.Join(strings.Fields(statement), " ")) + " "
		for _, forbidden := range []string{" DELETE ", " TRUNCATE "} {
			if strings.Contains(upper, forbidden) {
				t.Errorf("%s statement contains %s", name, forbidden)
			}
		}
	}
	if !strings.Contains(updatePurposeStatement, "version=$10") || !strings.Contains(updatePurposeStatement, "status='active'") {
		t.Fatalf("purpose update is missing optimistic/lifecycle guard: %q", updatePurposeStatement)
	}
	if !strings.Contains(updateRetentionStatement, "version=$9") || !strings.Contains(updateRetentionStatement, "status='active'") {
		t.Fatalf("retention update is missing optimistic/lifecycle guard: %q", updateRetentionStatement)
	}
}
