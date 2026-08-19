package entitlementrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEntitlementMigrationContainsEnforcement(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))
	b, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1/000018_entitlements.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"CREATE TABLE entitlement_rules", "CREATE TABLE entitlement_quota_policies", "CREATE TABLE entitlement_quota_counters", "CREATE TABLE entitlement_quota_usage", "FORCE ROW LEVEL SECURITY", "entitlement rule identity must remain stable", "quota counter cannot move backwards", "entitlement evidence cannot be hard-deleted"} {
		if !strings.Contains(s, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
	for _, bad := range []string{"plan_name", "if plan", "if plan"} {
		if strings.Contains(strings.ToLower(s), bad) {
			t.Fatalf("migration contains hard-coded plan branch %q", bad)
		}
	}
}
