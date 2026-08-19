package compliancerepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProductComplianceMigrationContainsRequiredGuards(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000017_product_compliance.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(data)
	for _, needle := range []string{"CREATE TABLE compliance_documents", "CREATE TABLE compliance_bindings", "CREATE TABLE compliance_policies", "CREATE TABLE compliance_verifications", "FORCE ROW LEVEL SECURITY", "compliance_subject_ref_exists", "compliance_requirements_valid", "compliance_no_delete"} {
		if !strings.Contains(sql, needle) {
			t.Fatalf("missing %q", needle)
		}
	}
}
