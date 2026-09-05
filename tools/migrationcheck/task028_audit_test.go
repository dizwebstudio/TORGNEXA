package main

import (
	"io"
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
		rootHandle, err := os.OpenRoot(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		err = inspectGoFiles(rootHandle, ".", func(path string, contents []byte) error {
			low := strings.ToLower(string(contents))
			for _, bad := range []string{"if plan ==", "if plan==", "switch plan", "enterprise_plan", "pro_plan", "free_plan"} {
				if strings.Contains(low, bad) {
					t.Fatalf("%s/%s contains hard-coded plan branch %q", rel, path, bad)
				}
			}
			return nil
		})
		_ = rootHandle.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func inspectGoFiles(root *os.Root, relative string, inspect func(string, []byte) error) error {
	directory, err := root.Open(relative)
	if err != nil {
		return err
	}
	entries, err := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	for _, entry := range entries {
		path := filepath.Join(relative, entry.Name())
		if entry.IsDir() {
			if err := inspectGoFiles(root, path, inspect); err != nil {
				return err
			}
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		file, err := root.Open(path)
		if err != nil {
			return err
		}
		contents, readErr := io.ReadAll(file)
		closeErr := file.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := inspect(path, contents); err != nil {
			return err
		}
	}
	return nil
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
