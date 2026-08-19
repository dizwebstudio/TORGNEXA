package uploads

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUploadPolicyContractKeepsConsumersReleaseGated(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "contracts", "upload", "upload-policy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, needle := range []string{
		"client_supplied_object_keys: false",
		"tenant_scoped_object_keys: true",
		"consumer_access_state: released",
		"release_authority: upload_security_pipeline_v1",
		"security_pipeline_complete: true",
		"scanner_failure_mode: retry_fail_closed",
		"immutable_security_evidence: true",
		"rescan_revokes_existing_release_before_scan: true",
		"consumer_must_revalidate_released_reference_before_read: true",
		"max_archive_entries: 10000",
		"max_parser_depth: 64",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("upload policy missing %q", needle)
		}
	}
	if strings.Contains(text, "consumer_access_state: clean") || strings.Contains(text, "consumer_access_state: quarantined") {
		t.Fatal("upload policy exposes a pre-release state to consumers")
	}
}
