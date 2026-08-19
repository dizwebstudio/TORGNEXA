package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTask088aFoundationRemainsFailClosedByItself(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	checks := map[string][]string{
		"migrations_legacy_pre_v1/000022_upload_quarantine_foundation.sql": {"FORCE ROW LEVEL SECURITY", "only received to quarantined is allowed before task 088b", "REVOKE DELETE, TRUNCATE ON uploads"},
		"contracts/upload/released-object-ref-v1.md":                       {"MUST receive upload content only through the upload `AccessGate`", "there is no public constructor"},
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
