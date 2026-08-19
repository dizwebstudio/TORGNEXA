package connectorrepo

import (
	"strings"
	"testing"
)

func TestRepositorySQLUsesTenantScopeAndOptimisticVersion(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) succeeded")
	}
	if !strings.Contains(applyScopeStatement, "app.organization_id") || !strings.Contains(applyScopeStatement, "app.workspace_id") || !strings.Contains(applyScopeStatement, "true)") {
		t.Fatalf("scope is not transaction local: %q", applyScopeStatement)
	}
	for name, statement := range map[string]string{"create": createAccountStatement, "status": changeStatusStatement, "health": recordHealthStatement, "capabilities": changeCapabilitiesStatement} {
		upper := " " + strings.ToUpper(strings.Join(strings.Fields(statement), " ")) + " "
		if strings.Contains(upper, " DELETE ") || strings.Contains(upper, " TRUNCATE ") {
			t.Fatalf("%s contains destructive SQL", name)
		}
	}
	if !strings.Contains(changeStatusStatement, "version=$5") || !strings.Contains(recordHealthStatement, "version=$7") {
		t.Fatal("account updates lack optimistic version guard")
	}
	if !strings.Contains(changeCapabilitiesStatement, "version=$4") || !strings.Contains(currentCapabilitiesStatement, "max(account_version)") || !strings.Contains(insertCapabilityRevisionStatement, "account_version") {
		t.Fatal("capability snapshots lack versioned optimistic persistence")
	}
	if !strings.Contains(listAccountsStatement, "id>$3") || !strings.Contains(listAccountsStatement, "LIMIT $4") {
		t.Fatal("account list is not cursor-bounded")
	}
	for _, forbidden := range []string{"password", "access_token", "refresh_token", "client_secret", "api_key"} {
		if strings.Contains(strings.ToLower(accountSelect), forbidden) || strings.Contains(strings.ToLower(createAccountStatement), forbidden) {
			t.Fatalf("plaintext credential column referenced: %s", forbidden)
		}
	}
}
