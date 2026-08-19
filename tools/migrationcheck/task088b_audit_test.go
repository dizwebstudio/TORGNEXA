package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTask088bUploadSecurityPipelineEvidence(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	checks := map[string][]string{
		"internal/platform/uploads/pipeline.go":                        {"type MalwareScanner interface", "inspectZip", "safeArchivePath", "validateJSON", "validateXML", "validateCSV", "RequestRescan", "DecisionClean", "DecisionRejected", "DecisionError"},
		"internal/platform/uploads/uploads.go":                         {"ValidateReleasedRef", "ValidateReleasedFor", "ReleasedObjectRef"},
		"internal/platform/uploads/clamav.go":                          {"zINSTREAM", "ClamAVScanner", "ErrScannerUnavailable"},
		"internal/platform/postgres/uploadrepo/repository.go":          {"RecordDecision", "MarkReleased", "RequestRescan", "upload_security_evidence", "security.upload.decision.v1", "security.upload.released.v1", "security.upload.rescan_requested.v1"},
		"migrations_legacy_pre_v1/000023_upload_security_pipeline.sql": {"CREATE TABLE upload_security_evidence", "FORCE ROW LEVEL SECURITY", "uploads_security_guard_update", "upload security evidence is immutable", "rescan must revoke released capability before scanning"},
		"contracts/upload/upload-policy.yaml":                          {"security_pipeline_complete: true", "scanner_failure_mode: retry_fail_closed", "immutable_security_evidence: true", "consumer_must_revalidate_released_reference_before_read: true"},
	}
	for rel, wants := range checks {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s missing %q", rel, want)
			}
		}
	}
}
