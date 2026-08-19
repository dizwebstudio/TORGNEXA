package approvalrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestApprovalMigrationHasRLSAppendOnlyAndQuorumGuards(t *testing.T) {
	_, src, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(src), "..", "..", "..", ".."))
	b, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000013_approval_engine.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToUpper(string(b))
	for _, want := range []string{"CREATE TABLE APPROVAL_POLICIES", "CREATE TABLE APPROVAL_REQUESTS", "CREATE TABLE APPROVAL_DECISIONS", "CREATE TABLE APPROVAL_ESCALATIONS", "FORCE ROW LEVEL SECURITY", "APPROVAL REQUEST QUORUM INCOMPLETE", "REQUESTER CANNOT APPROVE OWN REQUEST", "APPROVER SCOPE NOT ELIGIBLE FOR STAGE", "APPROVAL EVIDENCE IS APPEND-ONLY", "APPROVAL HISTORY CANNOT BE CLEARED", "FOR UPDATE"} {
		if !strings.Contains(s, want) {
			t.Errorf("migration missing %q", want)
		}
	}
	for _, bad := range []string{"FLOAT4", "FLOAT8", "DOUBLE PRECISION"} {
		if strings.Contains(s, bad) {
			t.Fatalf("provider/float construct %q", bad)
		}
	}
}
