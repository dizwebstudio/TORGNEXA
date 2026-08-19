package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTask082ProductComplianceBoundary(t *testing.T) {
	root := task082Root(t)
	checks := map[string][]string{
		"migrations_legacy_pre_v1/000017_product_compliance.sql": {"CREATE TABLE compliance_documents", "CREATE TABLE compliance_bindings", "CREATE TABLE compliance_policies", "CREATE TABLE compliance_verifications", "FORCE ROW LEVEL SECURITY", "compliance_subject_ref_exists", "compliance_holder_ref_exists"},
		"internal/platform/connectorsandbox/runtime.go":          {`operation.Capability == sdk.Capability("products.write")`, "product_compliance_denied", "session.guard == nil"},
		"internal/core/compliance/compliance.go":                 {"RegistryVerifier interface", "ExpiryNotifier interface", "OutcomeApproval", "expired_evidence", "unverified_evidence"},
	}
	for rel, needles := range checks {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, needle := range needles {
			if !strings.Contains(text, needle) {
				t.Fatalf("%s missing %q", rel, needle)
			}
		}
	}
	legacy := map[string]string{
		"contracts/compliance/product-compliance.schema.json":                "70922d54daf5b092f912f14b3516baee2036a3535a4d3e11a4f1adca753f5e13",
		"contracts/events/compliance-document-status-changed-v1.schema.json": "5463eda417d7d11f5ea8c32302021ce173d1ced468e61799f41bc3bcedac1ab5",
	}
	for rel, want := range legacy {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Fatalf("published contract drift %s: %s", rel, got)
		}
	}
}

func task082Root(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
