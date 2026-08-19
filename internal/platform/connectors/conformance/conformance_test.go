package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sandbox "github.com/torgnexa/torgnexa/internal/platform/connectorsandbox"
	"github.com/torgnexa/torgnexa/internal/platform/pluginsecurity"
)

type fakeIsolationCandidate struct {
	*ReferenceCandidate
	result sandbox.SandboxProbeResult
	err    error
}

func (candidate *fakeIsolationCandidate) IsolationProbe(context.Context, pluginsecurity.AdmissionPlan) (sandbox.SandboxProbeResult, error) {
	return candidate.result, candidate.err
}

func passingCandidate() Candidate {
	return &fakeIsolationCandidate{ReferenceCandidate: NewReferenceCandidate("unused"), result: sandbox.SandboxProbeResult{
		Report:    sandbox.ProbeReport{},
		Isolation: sandbox.IsolationEvidence{ProductionCredentialsBlocked: true, EnvironmentIsolated: true, FilesystemIsolated: true, DirectNetworkBlocked: true, EgressMediated: true, ResourceLimitsEnforced: true},
	}}
}

func tenants() (Tenant, Tenant) {
	return Tenant{OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"},
		Tenant{OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0004"}
}

func TestReferenceCandidatePassesRequiredSuite(t *testing.T) {
	primary, foreign := tenants()
	fixed := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	report := Run(context.Background(), passingCandidate(), primary, foreign, func() time.Time { return fixed })
	if err := Require(report); err != nil {
		t.Fatalf("reference conformance: %v report=%#v", err, report)
	}
	if len(report.Checks) != 13 {
		t.Fatalf("check count=%d", len(report.Checks))
	}
	if report.CompletedAt != fixed {
		t.Fatalf("completed_at=%s", report.CompletedAt)
	}
	var output bytes.Buffer
	if err := WriteJSON(&output, report); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, forbidden := range []string{"sandbox-only-reference-secret", "dry-run-secret-placeholder", "Authorization", "Bearer "} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("report leaked %q: %s", forbidden, text)
		}
	}
}

func TestIsolationFailureFailsClosedWithMachineReasonOnly(t *testing.T) {
	primary, foreign := tenants()
	candidate := &fakeIsolationCandidate{ReferenceCandidate: NewReferenceCandidate("unused"), err: errors.New("host raw secret=do-not-report")}
	report := Run(context.Background(), candidate, primary, foreign, func() time.Time { return time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC) })
	if report.Passed {
		t.Fatal("failed isolation unexpectedly passed")
	}
	if !errors.Is(Require(report), ErrConformanceFailed) {
		t.Fatalf("unexpected require result: %v", Require(report))
	}
	encoded, _ := jsonBytes(report)
	if strings.Contains(string(encoded), "do-not-report") {
		t.Fatalf("raw error leaked: %s", encoded)
	}
	last := report.Checks[len(report.Checks)-1]
	if last.ID != CheckIsolation || last.ReasonCode != "isolation_probe_failed" {
		t.Fatalf("unexpected isolation result: %#v", last)
	}
}

func TestReportDigestDetectsMutation(t *testing.T) {
	primary, foreign := tenants()
	report := Run(context.Background(), passingCandidate(), primary, foreign, func() time.Time { return time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC) })
	report.Checks[0].Status = StatusFail
	report.Checks[0].ReasonCode = "mutated"
	report.Passed = false
	if err := report.Validate(); err == nil {
		t.Fatal("mutated report retained valid digest")
	}
}

func TestRequiredChecksAreStableAndOrdered(t *testing.T) {
	expected := []CheckID{CheckManifestSDK, CheckAuthBoundary, CheckHealthNormalization, CheckNormalizedErrors, CheckRateLimitRetry, CheckIdempotency, CheckWebhookReplay, CheckTenantIsolation, CheckDryRunSuppression, CheckProductionCredential, CheckEgressGrant, CheckResourceLimit, CheckIsolation}
	actual := RequiredChecks()
	if len(actual) != len(expected) {
		t.Fatalf("len=%d", len(actual))
	}
	for i := range expected {
		if actual[i] != expected[i] {
			t.Fatalf("check[%d]=%s want %s", i, actual[i], expected[i])
		}
	}
	actual[0] = "tampered"
	if RequiredChecks()[0] != expected[0] {
		t.Fatal("required checks returned mutable backing slice")
	}
}

func jsonBytes(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	err := encoder.Encode(value)
	return buffer.Bytes(), err
}
