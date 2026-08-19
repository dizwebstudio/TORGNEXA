package backupdr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
}

func TestRestoreDrillKeepsSafetyControls(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "scripts", "check-postgres-backup-restore.sh")
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat restore drill: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("restore drill must not be a symlink")
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatal("restore drill must be executable")
	}
	// #nosec G304 -- path is a fixed repository-relative script beneath this test source.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read restore drill: %v", err)
	}
	text := string(data)
	required := []string{
		"--network none",
		"--read-only",
		"--cap-drop ALL",
		"--security-opt no-new-privileges",
		"NOBYPASSRLS",
		"pg_dump",
		"pg_restore --list",
		"pg_basebackup",
		"--wal-method stream",
		"--manifest-checksums SHA256",
		"pg_verifybackup",
		"migrations/catalog.json",
		"recovery_target_lsn",
		"at-recovery-target",
		"after-recovery-target",
	}
	for _, token := range required {
		if !strings.Contains(text, token) {
			t.Errorf("restore drill is missing safety control %q", token)
		}
	}
	forbidden := []string{
		"--network host",
		"PGPASSWORD",
		"pg_restore --clean",
		"archive_command = 'true'",
		"curl ",
		"|| true",
	}
	for _, token := range forbidden {
		if strings.Contains(text, token) {
			t.Errorf("restore drill contains forbidden pattern %q", token)
		}
	}
}

func TestRestoreEvidenceContractHasNoSensitiveLocationFields(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), "contracts", "operations", "postgresql-restore-evidence.schema.json")
	// #nosec G304 -- path is a fixed repository-relative contract beneath this test source.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read restore evidence schema: %v", err)
	}
	var schema any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("decode restore evidence schema: %v", err)
	}
	forbidden := map[string]struct{}{
		"credential":      {},
		"dsn":             {},
		"host":            {},
		"organization_id": {},
		"password":        {},
		"path":            {},
		"secret":          {},
		"storage_url":     {},
		"tenant_id":       {},
		"uri":             {},
		"username":        {},
		"workspace_id":    {},
	}
	var visit func(any)
	visit = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if properties, ok := typed["properties"].(map[string]any); ok {
				for name := range properties {
					if _, denied := forbidden[name]; denied {
						t.Errorf("restore evidence exposes forbidden property %q", name)
					}
				}
			}
			for _, child := range typed {
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		}
	}
	visit(schema)
}
