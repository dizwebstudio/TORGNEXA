package agentgovernancerepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMigrationContainsDurableFailClosedControls(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000026_ai_agent_governance.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		"CREATE TABLE ai_agent_policies",
		"CREATE TABLE ai_agent_kill_switches",
		"CREATE TABLE ai_agent_call_counters",
		"CREATE TABLE ai_agent_call_usage",
		"ai agent policy identity must remain stable",
		"ai agent governance evidence is append-only",
		"ai agent call counter cannot move backwards",
		"FORCE ROW LEVEL SECURITY",
		"ai agent governance evidence cannot be hard-deleted",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
