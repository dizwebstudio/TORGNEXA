package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTask028NoHardCodedPlanBranches(t *testing.T) {
	_, f, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(f), "../.."))
	paths := []string{"internal/platform/entitlements", "internal/platform/entitlementguard", "internal/platform/postgres/entitlementrepo"}
	for _, rel := range paths {
		err := filepath.Walk(filepath.Join(root, rel), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			low := strings.ToLower(string(b))
			for _, bad := range []string{"if plan ==", "if plan==", "switch plan", "enterprise_plan", "pro_plan", "free_plan"} {
				if strings.Contains(low, bad) {
					t.Fatalf("%s contains hard-coded plan branch %q", rel, bad)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
func TestTask028QuotaUsesAtomicLocks(t *testing.T) {
	_, f, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(f), "../.."))
	b, err := os.ReadFile(filepath.Join(root, "internal/platform/postgres/entitlementrepo/repository.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"pg_advisory_xact_lock", "FOR UPDATE", "entitlement_quota_usage", "ErrQuotaExceeded"} {
		if !strings.Contains(s, want) {
			t.Fatalf("quota repository missing %q", want)
		}
	}
}
