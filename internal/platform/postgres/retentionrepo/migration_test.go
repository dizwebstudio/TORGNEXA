package retentionrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRetentionMigrationSecurityAndEvidenceShape(t *testing.T) {
	t.Parallel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller unavailable")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", "..", "..", "migrations_legacy_pre_v1", "000039_retention_subject_requests_tenant_deletion.sql")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(data))
	required := []string{
		"create table privacy_subject_requests", "create table privacy_legal_holds", "create table privacy_execution_jobs",
		"create table privacy_execution_targets", "create table privacy_execution_evidence", "force row level security",
		"privacy_execution_evidence_append_only", "privacy_legal_hold_release_only", "subject_opaque_id",
		"archive_then_delete", "tenant_delete", "current_setting('app.organization_id',true)", "current_setting('app.workspace_id',true)",
	}
	for _, token := range required {
		if !strings.Contains(text, token) {
			t.Errorf("migration missing %q", token)
		}
	}
	for _, forbidden := range []string{"subject_email", "subject_phone", "full_name", "passport", "raw_payload", "raw_pii"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("migration contains raw subject-data token %q", forbidden)
		}
	}
}
