package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var task089bFloatPattern = regexp.MustCompile(`(?m)\bfloat(?:32|64)\b`)

func TestTask089bUsesExactArithmeticAndAppendOnlyEvidence(t *testing.T) {
	root := task089aRepositoryRoot(t)
	for _, relative := range []string{
		"internal/platform/fx/runtime.go",
		"internal/platform/fx/financial_converter.go",
		"internal/platform/fx/connector_adapter.go",
		"connectors/cbr-fx/rates.go",
	} {
		data := task089aRead(t, root, relative)
		if task089bFloatPattern.Match(data) {
			t.Fatalf("%s contains binary floating-point FX type", relative)
		}
	}
	sql := string(task089aRead(t, root, "migrations_legacy_pre_v1/000040_fx_rate_provider_completion.sql"))
	for _, required := range []string{"fx_rate_facts", "fx_resolution_evidence", "fx_conversion_records", "append-only", "REVOKE UPDATE, DELETE, TRUNCATE"} {
		if !strings.Contains(sql, required) {
			t.Fatalf("FX migration missing %q", required)
		}
	}
}

func TestTask089bCBRConformanceEvidencePasses(t *testing.T) {
	root := task089aRepositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "docs", "connectors", "cbr-fx", "conformance-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Passed bool `json:"passed"`
		Checks []struct {
			Status string `json:"status"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Checks) != 13 {
		t.Fatalf("invalid CBR conformance report: passed=%v checks=%d", report.Passed, len(report.Checks))
	}
	for i, check := range report.Checks {
		if check.Status != "pass" {
			t.Fatalf("CBR conformance check %d failed", i+1)
		}
	}
}
