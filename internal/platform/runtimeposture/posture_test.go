package runtimeposture

import (
	"errors"
	"testing"
	"time"
)

func TestEvaluateRequiresLeastPrivilegeAndPatchedRuntime(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	identity := DatabaseIdentity{Role: "torgnexa_app"}
	assessment := evaluate(identity, "go1.26.7", now)
	if assessment.Status != "pass" || !assessment.LeastPrivilege || !assessment.GoRuntimePatched {
		t.Fatalf("assessment = %#v", assessment)
	}

	identity.BypassRLS = true
	assessment = evaluate(identity, "go1.26.7", now)
	if assessment.Status != "fail" || assessment.LeastPrivilege {
		t.Fatalf("unsafe identity passed: %#v", assessment)
	}
	identity.BypassRLS = false
	assessment = evaluate(identity, "go1.26.5", now)
	if assessment.Status != "fail" || assessment.GoRuntimePatched {
		t.Fatalf("old runtime passed: %#v", assessment)
	}
}

func TestVersionAtLeast(t *testing.T) {
	for _, test := range []struct {
		actual string
		want   bool
	}{{"go1.26.7", true}, {"go1.26.8", true}, {"go1.27.0", true}, {"devel go1.27-abc", true}, {"go1.26.6", false}, {"unknown", false}} {
		if got := versionAtLeast(test.actual, MinimumGoVersion); got != test.want {
			t.Fatalf("versionAtLeast(%q) = %v, want %v", test.actual, got, test.want)
		}
	}
}

func TestScanIdentityRejectsNil(t *testing.T) {
	if !errors.Is(scanIdentity(nil, &DatabaseIdentity{}), ErrInvalid) {
		t.Fatal("nil row was accepted")
	}
}
