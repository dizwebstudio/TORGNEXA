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
		{name: "denied AND", expression: "MIT AND GPL-3.0-only", want: "is denied"},
		{name: "OR requires selection", expression: "MIT OR GPL-3.0-only", want: "explicit selected"},
		{name: "OR selected allowed", expression: "MIT OR GPL-3.0-only", selection: "MIT"},
		{name: "review required", expression: "MPL-2.0", want: "requires legal review"},
		{name: "Trivy public domain alias", expression: "Public Domain"},
		{name: "Trivy lowercase public domain alias", expression: "public-domain"},
		{name: "Trivy short public domain alias", expression: "Public"},
		{name: "Trivy bzip alias", expression: "bzip-2-1.0.6"},
		{name: "SPDX ICU", expression: "ICU"},
		{name: "Trivy GPLv3 alias", expression: "GPLv3+"},
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
			policy := fmt.Sprintf(`{"version":1,"allowed_spdx":["Apache-2.0","GPL-3.0-or-later","ICU","MIT","Public-Domain","bzip2-1.0.6"],"review_required_spdx":["MPL-2.0"],"denied_spdx":["GPL-3.0-only"],"selected_or_choices":%s,"unknown_license_policy":"deny"}`, selection)
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
	valid := []byte(`{"version":1,"allowed_spdx":["MIT"],"review_required_spdx":[],"denied_spdx":[],"selected_or_choices":[],"unknown_license_policy":"deny"}`)
	for _, test := range []struct {
		name   string
		policy []byte
		report []byte
	}{
		{name: "unknown policy field", policy: append([]byte(nil), []byte(`{"version":1,"allowed_spdx":[],"review_required_spdx":[],"denied_spdx":[],"selected_or_choices":[],"unknown_license_policy":"deny","bypass":true}`)...), report: []byte(`{"SchemaVersion":2,"Results":[]}`)},
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
