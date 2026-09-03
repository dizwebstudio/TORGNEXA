package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSmokeKeyChangesPerRun(t *testing.T) {
	base := config{ReleaseCommit: "0123456789abcdef0123456789abcdef01234567", AccountRef: "sandbox", RunID: "run-1"}
	if first, second := smokeKey(base, "stock-write"), smokeKey(base, "stock-write"); first != second {
		t.Fatalf("same run must produce a stable idempotency key: %q != %q", first, second)
	}
	base.RunID = "run-2"
	if first, second := smokeKey(config{ReleaseCommit: "0123456789abcdef0123456789abcdef01234567", AccountRef: "sandbox", RunID: "run-1"}, "stock-write"), smokeKey(base, "stock-write"); first == second {
		t.Fatalf("different runs must not reuse an idempotency key: %q", first)
	}
}

func TestGoldenPathFlowIsAllOrNothingAndRedacted(t *testing.T) {
	variables := map[string]string{
		"TORGNEXA_MARKETPLACE_SMOKE_FLOW_REF":        "golden-path/flow-01",
		"TORGNEXA_MARKETPLACE_SMOKE_ORDER_REF":       "order/flow-01",
		"TORGNEXA_MARKETPLACE_SMOKE_RESERVATION_REF": "reservation/flow-01",
		"TORGNEXA_MARKETPLACE_SMOKE_SHIPMENT_REF":    "shipment/flow-01",
		"TORGNEXA_MARKETPLACE_SMOKE_RETURN_REF":      "return/flow-01",
		"TORGNEXA_MARKETPLACE_SMOKE_REFUND_REF":      "refund/flow-01",
		"TORGNEXA_MARKETPLACE_SMOKE_SETTLEMENT_REF":  "settlement/flow-01",
		"TORGNEXA_MARKETPLACE_SMOKE_MARKING_REF":     "marking/flow-01",
		"TORGNEXA_MARKETPLACE_SMOKE_EDO_REF":         "edo/flow-01",
	}
	for name, value := range variables {
		t.Setenv(name, value)
	}
	flow, err := loadGoldenPathFlow()
	if err != nil || flow == nil || flow.OrderRef != "order/flow-01" || flow.EDORef != "edo/flow-01" {
		t.Fatalf("golden path flow = %+v, err=%v", flow, err)
	}
	t.Setenv("TORGNEXA_MARKETPLACE_SMOKE_EDO_REF", "")
	if _, err = loadGoldenPathFlow(); err == nil {
		t.Fatal("incomplete golden path flow was accepted")
	}
	t.Setenv("TORGNEXA_MARKETPLACE_SMOKE_EDO_REF", "edo/flow-01")
	t.Setenv("TORGNEXA_MARKETPLACE_SMOKE_ORDER_REF", "Bearer abc")
	if _, err = loadGoldenPathFlow(); err == nil {
		t.Fatal("unsafe golden path reference was accepted")
	}
}

func TestSecretRuntimeRequiresTheBoundSecretReference(t *testing.T) {
	runtime := secretRuntime{value: "synthetic-secret"}
	var received string
	if err := runtime.Secrets().UseSecret(t.Context(), smokeSecretReference, func(value []byte) error {
		received = string(value)
		return nil
	}); err != nil {
		t.Fatalf("bound secret reference rejected: %v", err)
	}
	if received != "synthetic-secret" {
		t.Fatalf("received secret=%q", received)
	}
}

func TestEvidenceIsRedactedAndWrittenWithPrivatePermissions(t *testing.T) {
	temporary := t.TempDir()
	path := filepath.Join(temporary, "nested", "marketplace-live-smoke.json")
	evidence := smokeEvidence{
		SchemaVersion:  1,
		Status:         "FAIL",
		Scope:          "qualification",
		Environment:    "non-production",
		Target:         "dedicated-non-production",
		Repository:     "owner/repository",
		ReleaseCommit:  "0123456789abcdef0123456789abcdef01234567",
		ConnectorID:    "marketplace-connector",
		AccountRef:     "sandbox-account",
		QualifiedAt:    "2026-09-03T10:00:00Z",
		CredentialMode: "env_only_secret_accessor",
		Taxonomy:       taxonomyEvidence{Status: "NOT_RUN"},
		Write:          writeEvidenceState{Restored: true},
		Failure:        &failureEvidence{CheckID: "configuration", ErrorCode: "credential_missing"},
	}
	if err := writeEvidence(path, evidence); err != nil {
		t.Fatalf("write evidence: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat evidence: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("evidence permissions = %o, want 600", got)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read evidence: %v", err)
	}
	var decoded smokeEvidence
	if err := json.Unmarshal(contents, &decoded); err != nil {
		t.Fatalf("parse evidence: %v", err)
	}
	for _, forbidden := range []string{"Authorization", "api_key", "client_id", "token", "raw_body", "remote_id", "quantity"} {
		if strings.Contains(strings.ToLower(string(contents)), strings.ToLower(forbidden)) {
			t.Fatalf("evidence contains forbidden sensitive field %q: %s", forbidden, contents)
		}
	}
}
