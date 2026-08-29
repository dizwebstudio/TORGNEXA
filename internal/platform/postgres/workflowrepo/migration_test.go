package workflowrepo

import (
	"os"
	"strings"
	"testing"
)

func TestWorkflowMigrationGuardsTenantAndHistory(t *testing.T) {
	data, err := os.ReadFile("../../../../migrations/000027_workflow_automation.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ToUpper(string(data))
	for _, needle := range []string{
		"CREATE TABLE WORKFLOWS", "CREATE TABLE WORKFLOW_VERSIONS", "CREATE TABLE WORKFLOW_RUNS",
		"CREATE TABLE WORKFLOW_STEP_RUNS", "CREATE TABLE WORKFLOW_STEP_EVIDENCE", "CREATE TABLE WORKFLOW_EVENT_RECEIPTS",
		"FORCE ROW LEVEL SECURITY", "CURRENT_SETTING('APP.ORGANIZATION_ID'", "CURRENT_SETTING('APP.WORKSPACE_ID'",
		"WORKFLOW HISTORICAL EVIDENCE IS IMMUTABLE", "REVOKE UPDATE,DELETE,TRUNCATE",
		"INSERT INTO MIGRATION_HISTORY",
	} {
		if !strings.Contains(source, needle) {
			t.Errorf("migration missing %q", needle)
		}
	}
}
