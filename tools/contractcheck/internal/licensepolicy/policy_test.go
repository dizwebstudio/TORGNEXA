package licensepolicy

import (
	"fmt"
	"strings"
	"testing"
)

func TestLicenseExpressions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		expression string
		selection  string
		want       string
	}{
		{name: "allowed atom", expression: "MIT"},
		{name: "allowed AND", expression: "MIT AND Apache-2.0"},
		{name: "denied AND", expression: "MIT AND AGPL-3.0-only", want: "is denied"},
		{name: "OR requires selection", expression: "MIT OR AGPL-3.0-only", want: "explicit selected"},
		{name: "OR selected allowed", expression: "MIT OR AGPL-3.0-only", selection: "MIT"},
		{name: "review required", expression: "MPL-2.0", want: "requires legal review"},
		{name: "SPDX GPL-3-only", expression: "GPL-3.0-only"},
		{name: "SPDX LGPL-2-or-later", expression: "LGPL-2.0-or-later"},
		{name: "SPDX curl", expression: "curl"},
		{name: "Trivy public domain alias", expression: "Public Domain"},
		{name: "Trivy lowercase public domain alias", expression: "public-domain"},
		{name: "Trivy short public domain alias", expression: "Public"},
		{name: "Trivy split public domain alias", expression: "Domain"},
		{name: "Trivy bzip alias", expression: "bzip-2-1.0.6"},
		{name: "SPDX ICU", expression: "ICU"},
		{name: "Trivy GPLv3 alias", expression: "GPLv3+"},
		{name: "SPDX Zlib", expression: "Zlib"},
		{name: "unknown", expression: "LicenseRef-Custom", want: "unknown and denied"},
		{name: "WITH rejected", expression: "Apache-2.0 WITH LLVM-exception", want: "WITH exceptions"},
		{name: "malformed", expression: "MIT OR", want: "invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			selection := "[]"
			if test.selection != "" {
				selection = fmt.Sprintf(`[{"expression":%q,"selected":%q}]`, test.expression, test.selection)
			}
			policy := fmt.Sprintf(`{"version":1,"allowed_spdx":["Apache-2.0","GPL-3.0-only","GPL-3.0-or-later","ICU","LGPL-2.0-or-later","MIT","Public-Domain","Zlib","bzip2-1.0.6","curl"],"review_required_spdx":["MPL-2.0"],"denied_spdx":["AGPL-3.0-only"],"approved_image_artifacts":[],"approved_trivy_license_expressions":[],"selected_or_choices":%s,"unknown_license_policy":"deny"}`, selection)
			report := fmt.Sprintf(`{"SchemaVersion":2,"Results":[{"Licenses":[{"Name":%q}]}]}`, test.expression)
			_, err := Check([]byte(policy), []byte(report))
			if test.want == "" {
				if err != nil {
					t.Fatalf("Check() error = %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Check() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestPolicyAndReportFailClosed(t *testing.T) {
	t.Parallel()
	valid := []byte(`{"version":1,"allowed_spdx":["MIT"],"review_required_spdx":[],"denied_spdx":[],"approved_image_artifacts":[],"approved_trivy_license_expressions":[],"selected_or_choices":[],"unknown_license_policy":"deny"}`)
	for _, test := range []struct {
		name   string
		policy []byte
		report []byte
	}{
		{name: "unknown policy field", policy: append([]byte(nil), []byte(`{"version":1,"allowed_spdx":[],"review_required_spdx":[],"denied_spdx":[],"approved_image_artifacts":[],"approved_trivy_license_expressions":[],"selected_or_choices":[],"unknown_license_policy":"deny","bypass":true}`)...), report: []byte(`{"SchemaVersion":2,"Results":[]}`)},
		{name: "missing report results", policy: valid, report: []byte(`{"SchemaVersion":2}`)},
		{name: "wrong report schema", policy: valid, report: []byte(`{"SchemaVersion":1,"Results":[]}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Check(test.policy, test.report); err == nil {
				t.Fatal("Check() unexpectedly passed")
			}
		})
	}
}

func TestDigestScopedTrivyExpressionApproval(t *testing.T) {
	t.Parallel()
	const artifact = "example.invalid/runtime:v1@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	legacyExpression := "ASL 1.1 and BSD with advertising"
	longExpression := strings.Repeat("Z", maxExpressionBytes+1)
	policy := fmt.Sprintf(`{"version":1,"allowed_spdx":["MIT"],"review_required_spdx":[],"denied_spdx":[],"approved_image_artifacts":[%q],"approved_trivy_license_expressions":[%q,%q],"selected_or_choices":[],"unknown_license_policy":"deny"}`, artifact, legacyExpression, longExpression)

	for _, expression := range []string{legacyExpression, longExpression} {
		report := fmt.Sprintf(`{"SchemaVersion":2,"ArtifactName":%q,"ArtifactType":"container_image","Results":[{"Licenses":[{"Name":%q}]}]}`, artifact, expression)
		if _, err := Check([]byte(policy), []byte(report)); err != nil {
			t.Fatalf("approved image expression %q failed: %v", expression, err)
		}
	}

	wrongArtifact := strings.Replace(artifact, strings.Repeat("a", 64), strings.Repeat("b", 64), 1)
	report := fmt.Sprintf(`{"SchemaVersion":2,"ArtifactName":%q,"ArtifactType":"container_image","Results":[{"Licenses":[{"Name":%q}]}]}`, wrongArtifact, legacyExpression)
	if _, err := Check([]byte(policy), []byte(report)); err == nil || !strings.Contains(err.Error(), "unexpected SPDX token") {
		t.Fatalf("digest-mismatched image Check() error = %v, want fail-closed parse error", err)
	}
}

func TestDigestScopedApprovalPolicyFailsClosed(t *testing.T) {
	t.Parallel()
	const artifact = "example.invalid/runtime:v1@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tests := []struct {
		name   string
		policy string
		want   string
	}{
		{
			name:   "mutable artifact",
			policy: `{"version":1,"allowed_spdx":[],"review_required_spdx":[],"denied_spdx":[],"approved_image_artifacts":["example.invalid/runtime:v1"],"approved_trivy_license_expressions":["legacy"],"selected_or_choices":[],"unknown_license_policy":"deny"}`,
			want:   "invalid digest-pinned image",
		},
		{
			name:   "missing expressions",
			policy: fmt.Sprintf(`{"version":1,"allowed_spdx":[],"review_required_spdx":[],"denied_spdx":[],"approved_image_artifacts":[%q],"approved_trivy_license_expressions":[],"selected_or_choices":[],"unknown_license_policy":"deny"}`, artifact),
			want:   "must either both be empty",
		},
		{
			name:   "unknown cannot be approved",
			policy: fmt.Sprintf(`{"version":1,"allowed_spdx":[],"review_required_spdx":[],"denied_spdx":[],"approved_image_artifacts":[%q],"approved_trivy_license_expressions":["UNKNOWN"],"selected_or_choices":[],"unknown_license_policy":"deny"}`, artifact),
			want:   "invalid expression",
		},
		{
			name:   "oversized expression",
			policy: fmt.Sprintf(`{"version":1,"allowed_spdx":[],"review_required_spdx":[],"denied_spdx":[],"approved_image_artifacts":[%q],"approved_trivy_license_expressions":[%q],"selected_or_choices":[],"unknown_license_policy":"deny"}`, artifact, strings.Repeat("A", maxApprovedTrivyExpressionBytes+1)),
			want:   "invalid expression",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidatePolicy([]byte(test.policy)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidatePolicy() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestApprovedImageArtifactMustRemainInRuntimeInventory(t *testing.T) {
	t.Parallel()
	const artifact = "example.invalid/runtime:v1@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	policy := fmt.Sprintf(`{"version":1,"allowed_spdx":[],"review_required_spdx":[],"denied_spdx":[],"approved_image_artifacts":[%q],"approved_trivy_license_expressions":["legacy"],"selected_or_choices":[],"unknown_license_policy":"deny"}`, artifact)
	if err := ValidatePolicyForImageArtifacts([]byte(policy), []string{artifact}); err != nil {
		t.Fatalf("ValidatePolicyForImageArtifacts() error = %v", err)
	}
	if err := ValidatePolicyForImageArtifacts([]byte(policy), nil); err == nil || !strings.Contains(err.Error(), "not present in the current runtime inventory") {
		t.Fatalf("ValidatePolicyForImageArtifacts() stale approval error = %v", err)
	}
}
