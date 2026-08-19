package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/platform/approval"
)

func TestTask017SensitiveWritesFailClosedWithoutPolicy(t *testing.T) {
	if approval.GateDecision(approval.RiskWriteSensitive, false) != approval.DecisionDeny {
		t.Fatal("sensitive write without policy must deny")
	}
	if approval.GateDecision(approval.RiskLegallySignificant, false) != approval.DecisionDeny {
		t.Fatal("legally significant write without policy must deny")
	}
	if approval.GateDecision(approval.RiskWriteSensitive, true) != approval.DecisionRequire {
		t.Fatal("sensitive write with policy must require approval")
	}
}
func TestTask017MigrationContainsDatabaseSideApprovalGuards(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	b, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000013_approval_engine.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, want := range []string{"requester cannot approve own request", "approver scope not eligible for stage", "approval request quorum incomplete", "approval evidence is append-only", "approval history cannot be cleared", "force row level security"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing database approval guard %q", want)
		}
	}
	for _, bad := range []string{"ozon", "wildberries", "marketplace_status", "float4", "float8", "double precision"} {
		if strings.Contains(s, bad) {
			t.Fatalf("approval engine leaked provider/float token %q", bad)
		}
	}
}
