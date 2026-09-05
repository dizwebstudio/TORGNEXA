package releasecheck

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

func TestValidateValidModes(t *testing.T) {
	t.Parallel()
	for _, mode := range []Mode{ModeDryRun, ModePublic} {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			bundle := newTestBundle(t, mode)
			bundle.writeManifest()
			result, err := Validate(context.Background(), bundle.options())
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if result.Status != "pass" || result.SubjectCount != 1 || result.RuntimeSubjectCount != 8 || result.ReportCount != 12 {
				t.Fatalf("unexpected result: %#v", result)
			}
			if mode == ModeDryRun {
				if result.PublicReady || result.ExternalIdentityVerification != "pending" || len(result.Blockers) != 3 {
					t.Fatalf("unexpected dry-run result: %#v", result)
				}
			} else if !result.PublicReady || !strings.Contains(result.ExternalIdentityVerification, "structurally bound") || len(result.Blockers) != 0 {
				t.Fatalf("public result did not flag external cryptographic verification: %#v", result)
			}
		})
	}
}

func TestValidateAcceptsExactUnexpiredException(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle(t, ModeDryRun)
	bundle.manifest.Exceptions = append(bundle.manifest.Exceptions, riskException{
		ID:            "SEC-123",
		Kind:          "dependency",
		Finding:       "GO-2026-0001",
		Scope:         "example.invalid/module@v1.2.3",
		Justification: "Synthetic acceptance fixture with bounded release risk.",
		Ticket:        "SEC-123",
		Approver:      "security-reviewers",
		ApprovedAt:    testNow.Add(-time.Hour).Format(time.RFC3339),
		ExpiresAt:     testNow.Add(24 * time.Hour).Format(time.RFC3339),
	})
	bundle.writeManifest()
	if _, err := Validate(context.Background(), bundle.options()); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsInvalidEvidence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mode   Mode
		mutate func(*testBundle)
		want   string
	}{
		{name: "manifest version", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Version = 2 }, want: "manifest version must be 1"},
		{name: "unknown manifest field", mode: ModeDryRun, mutate: func(b *testBundle) {
			b.rawManifest = func(data []byte) []byte {
				return []byte(strings.Replace(string(data), `"version": 1,`, `"version": 1, "unexpected": true,`, 1))
			}
		}, want: "unknown field"},
		{name: "duplicate manifest field", mode: ModeDryRun, mutate: func(b *testBundle) {
			b.rawManifest = func(data []byte) []byte {
				return []byte(strings.Replace(string(data), `"version": 1,`, `"version": 1, "version": 1,`, 1))
			}
		}, want: "duplicate JSON object key"},
		{name: "trailing manifest JSON", mode: ModeDryRun, mutate: func(b *testBundle) {
			b.rawManifest = func(data []byte) []byte { return append(data, []byte(` {}`)...) }
		}, want: "multiple JSON values"},
		{name: "missing array", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Exceptions = nil }, want: `field "exceptions" must be an array`},
		{name: "invalid semver", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Release.Version = "1.2.3" }, want: "canonical v-prefixed semantic version"},
		{name: "semver leading zero", mode: ModeDryRun, mutate: func(b *testBundle) { b.setVersion("v01.2.3") }, want: "canonical v-prefixed semantic version"},
		{name: "commit", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Release.Commit = strings.Repeat("A", 40) }, want: "lowercase 40-hex"},
		{name: "repository", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Release.Repository = "../repo" }, want: "canonical owner/name"},
		{name: "tag mismatch", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Release.Ref = "refs/tags/v9.9.9" }, want: "release.ref must equal"},
		{name: "workflow escape", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Release.Workflow = "../release.yml" }, want: "safe .github/workflows"},
		{name: "run ID", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Release.WorkflowRunID = "latest" }, want: "positive decimal run ID"},
		{name: "run URL repository mismatch", mode: ModeDryRun, mutate: func(b *testBundle) {
			b.manifest.Release.WorkflowRunURL = "https://github.com/other/repo/actions/runs/123456"
		}, want: "exact github.com repository and run ID"},
		{name: "dry run claims ready", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Release.PublicReady = boolPointer(true) }, want: "dry-run mode requires"},
		{name: "public not ready", mode: ModePublic, mutate: func(b *testBundle) { b.manifest.Release.PublicReady = boolPointer(false) }, want: "public mode requires"},
		{name: "subject traversal", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Subjects[0].Path = "../api" }, want: "path traversal"},
		{name: "subject absolute path", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Subjects[0].Path = "/tmp/api" }, want: "repository-relative"},
		{name: "subject type", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Subjects[0].Type = "container" }, want: "type must be binary"},
		{name: "subject platform", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Subjects[0].Platform = "any" }, want: "platform must be os/arch"},
		{name: "subject digest uppercase", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Subjects[0].SHA256 = strings.ToUpper(b.manifest.Subjects[0].SHA256) }, want: "64 lowercase hex"},
		{name: "subject digest mismatch", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Subjects[0].SHA256 = strings.Repeat("0", 64) }, want: "SHA-256 mismatch"},
		{name: "duplicate subject", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Subjects = append(b.manifest.Subjects, b.manifest.Subjects[0]) }, want: "duplicate subject"},
		{name: "runtime image tag only", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.RuntimeSubjects[0].Image = "postgres:18" }, want: "digest-pinned"},
		{name: "runtime platform missing", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.RuntimeSubjects = b.manifest.RuntimeSubjects[1:] }, want: "cover both"},
		{name: "runtime platform digest mismatch", mode: ModeDryRun, mutate: func(b *testBundle) {
			b.manifest.RuntimeSubjects[1].Image = "clickhouse/clickhouse-server:26.6@sha256:" + strings.Repeat("e", 64)
		}, want: "one image digest"},
		{name: "missing SBOM", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.SBOMs = []sbomReference{} }, want: "exactly one SBOM"},
		{name: "SBOM format", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.SBOMs[0].Format = "spdx-json" }, want: "SPDX-2.3-json"},
		{name: "SBOM subject digest", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.SBOMs[0].SubjectSHA256 = strings.Repeat("0", 64) }, want: "subject digest does not match"},
		{name: "SPDX version", mode: ModeDryRun, mutate: func(b *testBundle) { b.updateSPDX(func(doc *spdxDocument) { doc.SPDXVersion = "SPDX-2.2" }) }, want: "spdxVersion must be SPDX-2.3"},
		{name: "SPDX packages empty", mode: ModeDryRun, mutate: func(b *testBundle) { b.updateSPDX(func(doc *spdxDocument) { doc.Packages = []spdxPackage{} }) }, want: "packages must not be empty"},
		{name: "SPDX creators empty", mode: ModeDryRun, mutate: func(b *testBundle) { b.updateSPDX(func(doc *spdxDocument) { doc.CreationInfo.Creators = []string{} }) }, want: "creators must not be empty"},
		{name: "SPDX subject checksum", mode: ModeDryRun, mutate: func(b *testBundle) {
			b.updateSPDX(func(doc *spdxDocument) { doc.Packages[0].Checksums[0].ChecksumValue = strings.Repeat("0", 64) })
		}, want: "do not map subject"},
		{name: "runtime SBOM missing", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.SBOMs = b.manifest.SBOMs[:len(b.manifest.SBOMs)-1] }, want: "each binary and runtime subject"},
		{name: "runtime SBOM wrong image", mode: ModeDryRun, mutate: func(b *testBundle) {
			b.manifest.SBOMs[1].RuntimeImage = "postgres:18@sha256:" + strings.Repeat("f", 64)
		}, want: "exact immutable image"},
		{name: "missing runtime report", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Reports = b.manifest.Reports[:len(b.manifest.Reports)-1] }, want: "one container report per runtime subject"},
		{name: "wrong report subject", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Reports[0].Subject = "runtime" }, want: "unexpected report kind/subject"},
		{name: "report failed", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Reports[0].Status = "failed" }, want: "status must be pass"},
		{name: "report not sanitized", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Reports[0].Sanitized = boolPointer(false) }, want: "explicitly be sanitized"},
		{name: "report mutable tool", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Reports[0].ToolVersion = "latest" }, want: "tool_version must be immutable"},
		{name: "report missing database declaration", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Reports[0].Database = nil }, want: "database identity or explicit not_applicable"},
		{name: "dependency license policy missing", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.DependencyLicensePolicy = nil }, want: "dependency_license_policy is required"},
		{name: "dependency license policy path drift", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.DependencyLicensePolicy.RepositoryPath = "policy.json" }, want: "must bind supply-chain/license-policy.json"},
		{name: "denied dependency license", mode: ModeDryRun, mutate: func(b *testBundle) {
			report := validTrivyReport(false)
			report.Results = []trivyResult{{Licenses: []trivyFinding{{Name: "AGPL-3.0-only", Severity: "HIGH"}}}}
			b.updateReportJSON("license", "source", report)
		}, want: "SPDX identifier \"AGPL-3.0-only\" is denied"},
		{name: "dependency database N/A", mode: ModeDryRun, mutate: func(b *testBundle) { b.report("dependency", "source").Database = notApplicableDatabase() }, want: "requires vulnerability database identity"},
		{name: "container database stale", mode: ModeDryRun, mutate: func(b *testBundle) {
			b.firstContainerReport().Database.UpdatedAt = testNow.Add(-72 * time.Hour).Format(time.RFC3339)
		}, want: "database is stale"},
		{name: "database URI credentials", mode: ModeDryRun, mutate: func(b *testBundle) {
			b.report("dependency", "source").Database.URI = "https://token@example.invalid/db"
		}, want: "without credentials"},
		{name: "N/A database claims digest", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Reports[0].Database.Digest = strings.Repeat("d", 64) }, want: "must not claim identity fields"},
		{name: "report file digest", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Reports[0].SHA256 = strings.Repeat("0", 64) }, want: "file SHA-256 mismatch"},
		{name: "report duplicate JSON key", mode: ModeDryRun, mutate: func(b *testBundle) { b.updateReportRaw(0, []byte(`{"status":"pass","status":"pass"}`)) }, want: "duplicate JSON object key"},
		{name: "active secret finding", mode: ModeDryRun, mutate: func(b *testBundle) {
			report := validTrivyReport(false)
			report.Results = []trivyResult{{Secrets: []trivyFinding{{RuleID: "synthetic-secret"}}}}
			b.updateReportJSON("secret", "source", report)
		}, want: "active secret findings"},
		{name: "high SAST finding", mode: ModeDryRun, mutate: func(b *testBundle) {
			report := validGosecReport()
			report.Issues = []gosecIssue{{Severity: "HIGH", RuleID: "G999"}}
			report.Stats.Found = 1
			b.updateReportJSON("sast", "source", map[string]any{"version": 1, "reports": []any{report, validGosecReport()}})
		}, want: "blocking HIGH gosec"},
		{name: "gosec loading error", mode: ModeDryRun, mutate: func(b *testBundle) {
			report := validGosecReport()
			report.Errors["synthetic.go"] = []json.RawMessage{json.RawMessage(`{"error":"compile failed"}`)}
			b.updateReportJSON("sast", "source", map[string]any{"version": 1, "reports": []any{report, validGosecReport()}})
		}, want: "Go loading errors"},
		{name: "reachable Go vulnerability", mode: ModeDryRun, mutate: func(b *testBundle) {
			govuln := validGovulnReport()
			govuln = append(govuln, govulnMessage{Finding: &govulnFinding{OSV: "GO-2099-0001", Trace: []govulnFrame{{Module: "example.invalid/module", Package: "example.invalid/module/pkg", Function: "Vulnerable"}}}})
			b.updateReportJSON("dependency", "source", map[string]any{"version": 1, "reports": []any{govuln, validGovulnReport(), validTrivyReport(false)}})
		}, want: "reachable Go vulnerability"},
		{name: "high dependency vulnerability", mode: ModeDryRun, mutate: func(b *testBundle) {
			trivy := validTrivyReport(false)
			trivy.Results = []trivyResult{{Vulnerabilities: []trivyFinding{{VulnerabilityID: "CVE-2099-0001", Severity: "HIGH"}}}}
			b.updateReportJSON("dependency", "source", map[string]any{"version": 1, "reports": []any{validGovulnReport(), validGovulnReport(), trivy}})
		}, want: "blocking HIGH vulnerability"},
		{name: "wildcard exception", mode: ModeDryRun, mutate: func(b *testBundle) {
			b.manifest.Exceptions = []riskException{validException("example.invalid/module@*")}
		}, want: "exact component version or digest"},
		{name: "secret exception", mode: ModeDryRun, mutate: func(b *testBundle) {
			item := validException("example.invalid/module@v1.2.3")
			item.Kind = "secret"
			b.manifest.Exceptions = []riskException{item}
		}, want: "kind must be"},
		{name: "expired exception", mode: ModeDryRun, mutate: func(b *testBundle) {
			item := validException("example.invalid/module@v1.2.3")
			item.ExpiresAt = testNow.Add(-time.Minute).Format(time.RFC3339)
			b.manifest.Exceptions = []riskException{item}
		}, want: "is expired"},
		{name: "future approval", mode: ModeDryRun, mutate: func(b *testBundle) {
			item := validException("example.invalid/module@v1.2.3")
			item.ApprovedAt = testNow.Add(time.Hour).Format(time.RFC3339)
			b.manifest.Exceptions = []riskException{item}
		}, want: "approval is in the future"},
		{name: "public license missing", mode: ModePublic, mutate: func(b *testBundle) { b.manifest.License = nil }, want: "requires repository LICENSE"},
		{name: "public license source path", mode: ModePublic, mutate: func(b *testBundle) { b.manifest.License.RepositoryPath = "docs/LICENSE" }, want: "bind top-level LICENSE"},
		{name: "public license source commit", mode: ModePublic, mutate: func(b *testBundle) { b.manifest.License.SourceCommit = strings.Repeat("f", 40) }, want: "bind top-level LICENSE"},
		{name: "public license placeholder", mode: ModePublic, mutate: func(b *testBundle) { b.updateLicense([]byte("License decision required before publication.")) }, want: "unresolved decision marker"},
		{name: "public signature missing", mode: ModePublic, mutate: func(b *testBundle) { b.manifest.Signatures = []subjectEvidence{} }, want: "one signature for every"},
		{name: "public signature subject digest", mode: ModePublic, mutate: func(b *testBundle) { b.manifest.Signatures[0].SubjectSHA256 = strings.Repeat("0", 64) }, want: "subject digest does not match"},
		{name: "signature empty object", mode: ModePublic, mutate: func(b *testBundle) { b.updateSignature([]byte(`{}`)) }, want: "non-empty strict JSON"},
		{name: "public provenance missing", mode: ModePublic, mutate: func(b *testBundle) { b.manifest.Provenance = []subjectEvidence{} }, want: "one provenance for every"},
		{name: "public GitHub attestation missing", mode: ModePublic, mutate: func(b *testBundle) { b.manifest.GitHubAttestation = nil }, want: "requires a retained GitHub attestation"},
		{name: "provenance subject", mode: ModePublic, mutate: func(b *testBundle) {
			b.updateProvenance(func(doc *provenanceStatement) { doc.Subject[0].Digest["sha256"] = strings.Repeat("0", 64) })
		}, want: "provenance subject does not match"},
		{name: "provenance commit", mode: ModePublic, mutate: func(b *testBundle) {
			b.updateProvenance(func(doc *provenanceStatement) {
				doc.Predicate.BuildDefinition.ExternalParameters.Commit = strings.Repeat("f", 40)
			})
		}, want: "source identity does not match"},
		{name: "public external pending", mode: ModePublic, mutate: func(b *testBundle) { b.manifest.ExternalVerification = pendingExternalVerification() }, want: "requires external identity verification status pass"},
		{name: "public external tool latest", mode: ModePublic, mutate: func(b *testBundle) { b.manifest.ExternalVerification.ToolVersion = "latest" }, want: "tool_version must be exactly"},
		{name: "public external wrong issuer", mode: ModePublic, mutate: func(b *testBundle) {
			b.updateExternalVerification(func(record *externalVerificationRecord) { record.Issuer = "https://issuer.example.invalid" })
		}, want: "issuer is invalid"},
		{name: "public external wrong subject", mode: ModePublic, mutate: func(b *testBundle) {
			b.updateExternalVerification(func(record *externalVerificationRecord) { record.Subjects[0].SHA256 = strings.Repeat("0", 64) })
		}, want: "does not match retained bytes"},
		{name: "public external unverified attestation", mode: ModePublic, mutate: func(b *testBundle) {
			b.updateExternalVerification(func(record *externalVerificationRecord) {
				record.Subjects[0].GitHubAttestationVerified = boolPointer(false)
			})
		}, want: "is not fully verified"},
		{name: "public unresolved blocker", mode: ModePublic, mutate: func(b *testBundle) {
			b.manifest.Blockers = []blocker{{Code: "unexpected_blocker", Detail: "Synthetic unresolved release blocker."}}
		}, want: "does not permit unresolved blockers"},
		{name: "dry run missing public blocker", mode: ModeDryRun, mutate: func(b *testBundle) { b.removeBlocker("public_release_not_ready") }, want: "public_release_not_ready"},
		{name: "dry run missing license blocker", mode: ModeDryRun, mutate: func(b *testBundle) { b.removeBlocker("repository_license_unresolved") }, want: "repository_license_unresolved"},
		{name: "duplicate evidence path", mode: ModeDryRun, mutate: func(b *testBundle) { b.manifest.Reports[0].Path = b.manifest.Subjects[0].Path }, want: "is reused"},
		{name: "unbound file", mode: ModeDryRun, mutate: func(b *testBundle) { b.write("extra.txt", []byte("unbound")) }, want: "unbound evidence file"},
		{name: "subject symlink", mode: ModeDryRun, mutate: func(b *testBundle) {
			path := filepath.Join(b.root, filepath.FromSlash(b.manifest.Subjects[0].Path))
			if err := os.Remove(path); err != nil {
				t.Fatalf("remove subject: %v", err)
			}
			if err := os.Symlink("missing", path); err != nil {
				t.Fatalf("symlink subject: %v", err)
			}
		}, want: "symlink"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			bundle := newTestBundle(t, test.mode)
			test.mutate(bundle)
			bundle.writeManifest()
			_, err := Validate(context.Background(), bundle.options())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestValidateRejectsRootAndManifestPathHazards(t *testing.T) {
	t.Parallel()
	bundle := newTestBundle(t, ModeDryRun)
	bundle.writeManifest()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Validate(ctx, bundle.options())
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("canceled validation error = %v", err)
	}

	options := bundle.options()
	options.ManifestPath = "../evidence.json"
	if _, err := Validate(context.Background(), options); err == nil || !strings.Contains(err.Error(), "path traversal") {
		t.Fatalf("unsafe manifest error = %v", err)
	}

	link := filepath.Join(t.TempDir(), "bundle-link")
	if err := os.Symlink(bundle.root, link); err != nil {
		t.Fatalf("create root symlink: %v", err)
	}
	options = bundle.options()
	options.Root = link
	if _, err := Validate(context.Background(), options); err == nil || !strings.Contains(err.Error(), "not a symlink") {
		t.Fatalf("root symlink error = %v", err)
	}

	if _, err := Validate(nil, bundle.options()); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("nil context error = %v", err)
	}
}

func TestPolicyParsers(t *testing.T) {
	t.Parallel()
	semvers := []struct {
		value string
		valid bool
	}{
		{value: "v1.2.3", valid: true},
		{value: "v0.0.0-rc.1+build.7", valid: true},
		{value: "1.2.3", valid: false},
		{value: "v1.2", valid: false},
		{value: "v1.02.3", valid: false},
		{value: "v1.2.3-01", valid: false},
		{value: "v1.2.3+", valid: false},
	}
	for _, test := range semvers {
		if got := validSemver(test.value); got != test.valid {
			t.Errorf("validSemver(%q) = %v, want %v", test.value, got, test.valid)
		}
	}
	paths := []struct {
		value string
		valid bool
	}{
		{value: "reports/result.json", valid: true},
		{value: "", valid: false},
		{value: "/absolute", valid: false},
		{value: "../escape", valid: false},
		{value: "reports/../escape", valid: false},
		{value: `reports\escape`, valid: false},
		{value: "reports/control\n", valid: false},
	}
	for _, test := range paths {
		_, err := safeRelativePath(test.value)
		if (err == nil) != test.valid {
			t.Errorf("safeRelativePath(%q) error = %v, valid %v", test.value, err, test.valid)
		}
	}
	if _, err := ParseMode("DRY-RUN"); err == nil {
		t.Fatal("ParseMode accepted a non-canonical mode")
	}
}

type testBundle struct {
	t           *testing.T
	root        string
	mode        Mode
	manifest    manifestV1
	rawManifest func([]byte) []byte
}

func newTestBundle(t *testing.T, mode Mode) *testBundle {
	t.Helper()
	bundle := &testBundle{t: t, root: t.TempDir(), mode: mode}
	artifact := []byte("synthetic TORGNEXA api binary\n")
	artifactPath := "artifacts/api-linux-amd64"
	bundle.write(artifactPath, artifact)
	artifactDigest := digestBytes(artifact)
	ready := mode == ModePublic
	release := &releaseMetadata{
		Version:        "v1.2.3-rc.1",
		Commit:         strings.Repeat("a", 40),
		Repository:     "torgnexa/torgnexa",
		Ref:            "refs/tags/v1.2.3-rc.1",
		Workflow:       ".github/workflows/release.yml",
		WorkflowRunID:  "123456",
		WorkflowRunURL: "https://github.com/torgnexa/torgnexa/actions/runs/123456",
		PublicReady:    boolPointer(ready),
	}
	artifactSubject := subject{Name: "api", Type: "binary", Platform: "linux/amd64", Path: artifactPath, SHA256: artifactDigest}
	bundle.manifest = manifestV1{
		Version:         1,
		Release:         release,
		Subjects:        []subject{artifactSubject},
		RuntimeSubjects: makeRuntimeSubjects(),
		SBOMs:           []sbomReference{},
		Reports:         []reportReference{},
		Signatures:      []subjectEvidence{},
		Provenance:      []subjectEvidence{},
		Exceptions:      []riskException{},
		Blockers:        []blocker{},
	}
	policy := []byte(`{"version":1,"allowed_spdx":["Apache-2.0","MIT"],"review_required_spdx":[],"denied_spdx":["AGPL-3.0-only"],"approved_image_artifacts":[],"approved_trivy_license_expressions":[],"selected_or_choices":[],"unknown_license_policy":"deny"}`)
	policyPath := "policy/license-policy.json"
	bundle.write(policyPath, policy)
	bundle.manifest.DependencyLicensePolicy = &fileEvidence{
		Path: policyPath, SHA256: digestBytes(policy), RepositoryPath: "supply-chain/license-policy.json", SourceCommit: release.Commit,
	}

	spdx := spdxDocument{
		SPDXVersion:       "SPDX-2.3",
		DataLicense:       "CC0-1.0",
		SPDXID:            "SPDXRef-DOCUMENT",
		Name:              "api SBOM",
		DocumentNamespace: "https://torgnexa.local/spdx/api/v1.2.3-rc.1",
		CreationInfo: spdxCreationInfo{
			Created:  testNow.Add(-time.Hour).Format(time.RFC3339),
			Creators: []string{"Tool: synthetic-syft-1.0.0"},
		},
		Packages: []spdxPackage{{
			Name:   "api",
			SPDXID: "SPDXRef-Package-api",
			Checksums: []spdxChecksum{{
				Algorithm:     "SHA256",
				ChecksumValue: artifactDigest,
			}},
		}},
	}
	sbomPath := "sbom/api.spdx.json"
	sbomDigest := bundle.writeJSON(sbomPath, spdx)
	bundle.manifest.SBOMs = append(bundle.manifest.SBOMs, sbomReference{
		Subject: "api", Format: "SPDX-2.3-json", Path: sbomPath, SHA256: sbomDigest, SubjectSHA256: artifactDigest,
	})
	for _, runtime := range bundle.manifest.RuntimeSubjects {
		key := runtimeSubjectKey(runtime)
		stem := strings.NewReplacer("@", "-", "/", "-").Replace(key)
		runtimeSPDX := spdxDocument{
			SPDXVersion:       "SPDX-2.3",
			DataLicense:       "CC0-1.0",
			SPDXID:            "SPDXRef-DOCUMENT",
			Name:              runtime.Image,
			DocumentNamespace: "https://torgnexa.local/spdx/runtime/" + stem,
			CreationInfo: spdxCreationInfo{
				Created:  testNow.Add(-time.Hour).Format(time.RFC3339),
				Creators: []string{"Tool: synthetic-syft-1.0.0"},
			},
			Packages: []spdxPackage{{Name: runtime.Name, SPDXID: "SPDXRef-Package-" + stem}},
		}
		path := "sbom/" + stem + ".spdx.json"
		digest := bundle.writeJSON(path, runtimeSPDX)
		bundle.manifest.SBOMs = append(bundle.manifest.SBOMs, sbomReference{
			RuntimeSubject: key,
			RuntimeImage:   runtime.Image,
			Format:         "SPDX-2.3-json",
			Path:           path,
			SHA256:         digest,
		})
	}

	for _, kind := range []string{"secret", "sast", "dependency", "license"} {
		bundle.addReport(kind, "source")
	}
	for _, runtime := range bundle.manifest.RuntimeSubjects {
		bundle.addReport("container", runtimeSubjectKey(runtime))
	}

	if mode == ModePublic {
		license := []byte("Synthetic permissive test license text. Permission is granted for test fixtures only.\n")
		licensePath := "repository/LICENSE"
		bundle.write(licensePath, license)
		bundle.manifest.License = &fileEvidence{
			Path: licensePath, SHA256: digestBytes(license), RepositoryPath: "LICENSE", SourceCommit: release.Commit,
		}
		signaturePath := "signatures/api.sigstore.json"
		signatureDigest := bundle.writeJSON(signaturePath, map[string]any{
			"mediaType":        "application/vnd.dev.sigstore.bundle+json;version=0.3",
			"messageSignature": map[string]any{"subjectDigest": artifactDigest},
		})
		bundle.manifest.Signatures = append(bundle.manifest.Signatures, subjectEvidence{
			Subject: "api", Path: signaturePath, SHA256: signatureDigest, SubjectSHA256: artifactDigest,
		})
		provenance := validProvenance(artifactSubject, *release)
		provenancePath := "provenance/api.intoto.json"
		provenanceDigest := bundle.writeJSON(provenancePath, provenance)
		bundle.manifest.Provenance = append(bundle.manifest.Provenance, subjectEvidence{
			Subject: "api", Path: provenancePath, SHA256: provenanceDigest, SubjectSHA256: artifactDigest,
		})
		attestationPath := "attestations/github-attestation.json"
		attestationDigest := bundle.writeJSON(attestationPath, map[string]any{
			"mediaType":            "application/vnd.dev.sigstore.bundle.v0.3+json",
			"verificationMaterial": map[string]any{"synthetic": true},
		})
		bundle.manifest.GitHubAttestation = &digestEvidence{Path: attestationPath, SHA256: attestationDigest}
		externalPath := "verification/external-identity.json"
		externalDigest := bundle.writeJSON(externalPath, externalVerificationRecord{
			Version:                 1,
			Status:                  "pass",
			Repository:              release.Repository,
			SourceRevision:          release.Commit,
			Ref:                     release.Ref,
			Workflow:                release.Workflow,
			EventName:               "push",
			Issuer:                  "https://token.actions.githubusercontent.com",
			CertificateIdentity:     "https://github.com/" + release.Repository + "/" + release.Workflow + "@" + release.Ref,
			GitHubAttestationSHA256: attestationDigest,
			Subjects: []externalVerifiedSubject{{
				Name:                      "api",
				SHA256:                    artifactDigest,
				SignatureBundleSHA256:     signatureDigest,
				SignatureVerified:         boolPointer(true),
				GitHubAttestationVerified: boolPointer(true),
			}},
		})
		bundle.manifest.ExternalVerification = &externalVerification{
			Status: "pass", Tool: "cosign", ToolVersion: "v3.1.3", Path: externalPath, SHA256: externalDigest,
		}
	} else {
		bundle.manifest.ExternalVerification = pendingExternalVerification()
		bundle.manifest.Blockers = []blocker{
			{Code: "external_identity_verification_pending", Detail: "Protected OIDC identity verification is pending."},
			{Code: "public_release_not_ready", Detail: "This bundle is a non-public rehearsal."},
			{Code: "repository_license_unresolved", Detail: "Repository license approval remains unresolved."},
		}
	}
	return bundle
}

func (bundle *testBundle) updateExternalVerification(mutate func(*externalVerificationRecord)) {
	bundle.t.Helper()
	reference := bundle.manifest.ExternalVerification
	path := filepath.Join(bundle.root, filepath.FromSlash(reference.Path))
	// #nosec G304 -- bundle.root is a test-owned TempDir and reference.Path came from the synthetic manifest.
	data, err := os.ReadFile(path)
	if err != nil {
		bundle.t.Fatalf("read external verification: %v", err)
	}
	var record externalVerificationRecord
	if err := json.Unmarshal(data, &record); err != nil {
		bundle.t.Fatalf("decode external verification: %v", err)
	}
	mutate(&record)
	reference.SHA256 = bundle.writeJSON(reference.Path, record)
}

func makeRuntimeSubjects() []runtimeSubject {
	images := []struct{ name, image string }{
		{"clickhouse", "clickhouse/clickhouse-server:26.6@sha256:" + strings.Repeat("a", 64)},
		{"kafka", "apache/kafka:4.3.1@sha256:" + strings.Repeat("b", 64)},
		{"postgres", "postgres:18-alpine@sha256:" + strings.Repeat("c", 64)},
		{"valkey", "valkey/valkey:9.1.1-alpine@sha256:" + strings.Repeat("d", 64)},
	}
	result := make([]runtimeSubject, 0, len(images)*2)
	for _, image := range images {
		for _, platform := range []string{"linux/amd64", "linux/arm64"} {
			result = append(result, runtimeSubject{Name: image.name, Platform: platform, Image: image.image})
		}
	}
	return result
}

func (bundle *testBundle) addReport(kind, subject string) {
	bundle.t.Helper()
	path := "reports/" + strings.NewReplacer("@", "-", "/", "-").Replace(kind+"-"+subject) + ".json"
	var payload any
	switch kind {
	case "secret", "license":
		payload = validTrivyReport(false)
	case "sast":
		payload = map[string]any{"version": 1, "reports": []any{validGosecReport(), validGosecReport()}}
	case "dependency":
		payload = map[string]any{"version": 1, "reports": []any{validGovulnReport(), validGovulnReport(), validTrivyReport(false)}}
	case "container":
		payload = map[string]any{"version": 1, "reports": []any{validTrivyReport(true), validTrivyReport(true), validTrivyReport(true)}}
	default:
		bundle.t.Fatalf("unsupported synthetic report kind %q", kind)
	}
	digest := bundle.writeJSON(path, payload)
	database := notApplicableDatabase()
	if kind == "dependency" || kind == "container" {
		database = &databaseIdentity{
			Status:    "present",
			URI:       "https://vulnerability.example.invalid/db",
			UpdatedAt: testNow.Add(-time.Hour).Format(time.RFC3339),
			Digest:    strings.Repeat("e", 64),
		}
	}
	bundle.manifest.Reports = append(bundle.manifest.Reports, reportReference{
		Kind: kind, Subject: subject, Status: "pass", Tool: "synthetic-scanner", ToolVersion: "v1.2.3",
		Path: path, SHA256: digest, Sanitized: boolPointer(true), Database: database,
	})
}

func validTrivyReport(container bool) trivyReport {
	artifactType := "filesystem"
	if container {
		artifactType = "container_image"
	}
	return trivyReport{SchemaVersion: 2, ArtifactType: artifactType, Results: []trivyResult{}}
}

func validGosecReport() gosecReport {
	return gosecReport{Errors: map[string][]json.RawMessage{}, Issues: []gosecIssue{}, Stats: gosecStats{Found: 0}}
}

func validGovulnReport() []govulnMessage {
	return []govulnMessage{{Config: &govulnConfig{
		ProtocolVersion: "v1.0.0",
		ScannerName:     "govulncheck",
		ScannerVersion:  "v1.6.0",
		DB:              "https://vuln.go.dev",
		DBLastModified:  testNow.Add(-time.Hour).Format(time.RFC3339),
		ScanLevel:       "symbol",
		ScanMode:        "source",
	}}}
}

func notApplicableDatabase() *databaseIdentity {
	return &databaseIdentity{Status: "not_applicable", Reason: "Scanner has no external vulnerability database."}
}

func pendingExternalVerification() *externalVerification {
	return &externalVerification{Status: "pending"}
}

func validException(scope string) riskException {
	return riskException{
		ID:            "SEC-123",
		Kind:          "dependency",
		Finding:       "GO-2026-0001",
		Scope:         scope,
		Justification: "Synthetic acceptance fixture with bounded release risk.",
		Ticket:        "SEC-123",
		Approver:      "security-reviewers",
		ApprovedAt:    testNow.Add(-time.Hour).Format(time.RFC3339),
		ExpiresAt:     testNow.Add(24 * time.Hour).Format(time.RFC3339),
	}
}

func validProvenance(item subject, release releaseMetadata) provenanceStatement {
	return provenanceStatement{
		Type:          "https://in-toto.io/Statement/v1",
		Subject:       []provenanceSubject{{Name: item.Name, Digest: map[string]string{"sha256": item.SHA256}}},
		PredicateType: "https://slsa.dev/provenance/v1",
		Predicate: provenancePredicate{
			BuildDefinition: provenanceBuildDefinition{
				BuildType: "https://torgnexa.local/build/go/v1",
				ExternalParameters: provenanceExternalParameters{
					Repository: release.Repository, Commit: release.Commit, Ref: release.Ref, Workflow: release.Workflow,
					WorkflowRunID: release.WorkflowRunID, WorkflowRunURL: release.WorkflowRunURL,
				},
			},
			RunDetails: provenanceRunDetails{Builder: provenanceBuilder{ID: "https://github.com/actions/runner"}},
		},
	}
}

func (bundle *testBundle) options() Options {
	return Options{Root: bundle.root, ManifestPath: "evidence.json", Mode: bundle.mode, Now: testNow}
}

func (bundle *testBundle) writeManifest() {
	bundle.t.Helper()
	data, err := json.MarshalIndent(bundle.manifest, "", "  ")
	if err != nil {
		bundle.t.Fatalf("marshal manifest: %v", err)
	}
	data = append(data, '\n')
	if bundle.rawManifest != nil {
		data = bundle.rawManifest(data)
	}
	bundle.write("evidence.json", data)
}

func (bundle *testBundle) write(relative string, data []byte) {
	bundle.t.Helper()
	path := filepath.Join(bundle.root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		bundle.t.Fatalf("mkdir %s: %v", relative, err)
	}
	// #nosec G703 -- bundle.root is t.TempDir and relative is validated synthetic fixture data.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		bundle.t.Fatalf("write %s: %v", relative, err)
	}
}

func (bundle *testBundle) writeJSON(relative string, value any) string {
	bundle.t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		bundle.t.Fatalf("marshal %s: %v", relative, err)
	}
	data = append(data, '\n')
	bundle.write(relative, data)
	return digestBytes(data)
}

func (bundle *testBundle) setVersion(version string) {
	bundle.manifest.Release.Version = version
	bundle.manifest.Release.Ref = "refs/tags/" + version
}

func (bundle *testBundle) report(kind, subject string) *reportReference {
	bundle.t.Helper()
	for index := range bundle.manifest.Reports {
		if bundle.manifest.Reports[index].Kind == kind && bundle.manifest.Reports[index].Subject == subject {
			return &bundle.manifest.Reports[index]
		}
	}
	bundle.t.Fatalf("report %s/%s not found", kind, subject)
	return nil
}

func (bundle *testBundle) firstContainerReport() *reportReference {
	bundle.t.Helper()
	for index := range bundle.manifest.Reports {
		if bundle.manifest.Reports[index].Kind == "container" {
			return &bundle.manifest.Reports[index]
		}
	}
	bundle.t.Fatal("container report not found")
	return nil
}

func (bundle *testBundle) updateSPDX(mutate func(*spdxDocument)) {
	bundle.t.Helper()
	path := bundle.manifest.SBOMs[0].Path
	data, err := os.ReadFile(filepath.Join(bundle.root, filepath.FromSlash(path)))
	if err != nil {
		bundle.t.Fatalf("read SPDX: %v", err)
	}
	var document spdxDocument
	if err := json.Unmarshal(data, &document); err != nil {
		bundle.t.Fatalf("decode SPDX: %v", err)
	}
	mutate(&document)
	bundle.manifest.SBOMs[0].SHA256 = bundle.writeJSON(path, document)
}

func (bundle *testBundle) updateReportRaw(index int, data []byte) {
	bundle.t.Helper()
	path := bundle.manifest.Reports[index].Path
	bundle.write(path, data)
	bundle.manifest.Reports[index].SHA256 = digestBytes(data)
}

func (bundle *testBundle) updateReportJSON(kind, subject string, value any) {
	bundle.t.Helper()
	reference := bundle.report(kind, subject)
	reference.SHA256 = bundle.writeJSON(reference.Path, value)
}

func (bundle *testBundle) updateLicense(data []byte) {
	bundle.t.Helper()
	path := bundle.manifest.License.Path
	bundle.write(path, data)
	bundle.manifest.License.SHA256 = digestBytes(data)
}

func (bundle *testBundle) updateSignature(data []byte) {
	bundle.t.Helper()
	path := bundle.manifest.Signatures[0].Path
	bundle.write(path, data)
	bundle.manifest.Signatures[0].SHA256 = digestBytes(data)
}

func (bundle *testBundle) updateProvenance(mutate func(*provenanceStatement)) {
	bundle.t.Helper()
	path := bundle.manifest.Provenance[0].Path
	data, err := os.ReadFile(filepath.Join(bundle.root, filepath.FromSlash(path)))
	if err != nil {
		bundle.t.Fatalf("read provenance: %v", err)
	}
	var document provenanceStatement
	if err := json.Unmarshal(data, &document); err != nil {
		bundle.t.Fatalf("decode provenance: %v", err)
	}
	mutate(&document)
	bundle.manifest.Provenance[0].SHA256 = bundle.writeJSON(path, document)
}

func (bundle *testBundle) removeBlocker(code string) {
	bundle.t.Helper()
	result := bundle.manifest.Blockers[:0]
	for _, item := range bundle.manifest.Blockers {
		if item.Code != code {
			result = append(result, item)
		}
	}
	bundle.manifest.Blockers = result
}

func boolPointer(value bool) *bool {
	return &value
}

func TestErrorsPreserveCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	bundle := newTestBundle(t, ModeDryRun)
	bundle.writeManifest()
	_, err := Validate(ctx, bundle.options())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v does not wrap context.Canceled", err)
	}
}
