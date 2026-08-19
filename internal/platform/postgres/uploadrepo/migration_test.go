package uploadrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration(t *testing.T, name string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "migrations_legacy_pre_v1", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ToLower(string(raw))
}

func TestUploadFoundationMigrationIsTenantScopedAndFailClosed(t *testing.T) {
	text := readMigration(t, "000022_upload_quarantine_foundation.sql")
	for _, needle := range []string{"create table uploads", "force row level security", "uploads_tenant_all", "quarantine/", "released/", "security_evidence_id", "uploads_lifecycle_shape_chk", "only received to quarantined is allowed before task 088b", "revoke delete, truncate on uploads", "insert into migration_history"} {
		if !strings.Contains(text, needle) {
			t.Errorf("foundation migration missing %q", needle)
		}
	}
}

func TestUploadSecurityMigrationAddsImmutableEvidenceAndFullGuard(t *testing.T) {
	text := readMigration(t, "000023_upload_security_pipeline.sql")
	for _, needle := range []string{
		"create table upload_security_evidence", "force row level security", "upload_security_evidence_tenant_all",
		"scanner_status in ('clean','infected','error','not_run')", "decision in ('clean','rejected','error')",
		"drop trigger uploads_foundation_guard_update", "uploads_security_guard_update",
		"invalid upload security state transition", "rescan must revoke released capability before scanning",
		"upload_security_evidence_guard_insert", "invalid upload security check evidence",
		"upload security evidence is immutable", "revoke update, delete, truncate on upload_security_evidence",
		"uploads_security_evidence_fk", "insert into migration_history",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("security migration missing %q", needle)
		}
	}
	for _, forbidden := range []string{"client_object_key", "signed_url", "scanner_secret", "file_content text"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("forbidden storage/security bypass token %q", forbidden)
		}
	}
}

func TestRepositoryUsesTenantPredicatesTransactionalOutboxAndAuthorizedRelease(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "repository.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, needle := range []string{
		"organization_id=$1 and workspace_id=$2", "state='received'", "state='quarantined'", "state='released'",
		"upload_security_evidence", "newtransactionenqueuer", "security.upload.quarantined.v1", "security.upload.decision.v1",
		"security.upload.released.v1", "security.upload.rescan_requested.v1",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("repository missing %q", needle)
		}
	}
}
