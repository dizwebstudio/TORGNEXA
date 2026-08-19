package checker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRepositoryContracts(t *testing.T) {
	root := findRepositoryRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Check(ctx, root); err != nil {
		t.Fatalf("repository contracts failed validation: %v", err)
	}
}

func TestCheckValidatesInputsAndCancellation(t *testing.T) {
	t.Parallel()
	assertErrorContains(t, Check(nil, "."), "context is required")
	assertErrorContains(t, Check(context.Background(), ""), "repository root is required")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Check(ctx, findRepositoryRoot(t))
	assertErrorContains(t, err, "validation interrupted")
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("context state changed: %v", ctx.Err())
	}
}

func TestScanRepositoryRejectsUnsafeFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	contracts := filepath.Join(root, "contracts")
	mustMkdirAll(t, filepath.Join(contracts, "openapi"))
	mustMkdirAll(t, filepath.Join(contracts, "events"))
	mustWriteFile(t, filepath.Join(contracts, "openapi", "spec.json"), []byte(`{}`))
	mustWriteFile(t, filepath.Join(contracts, "events", "oversized.schema.json"), make([]byte, maxContractSize+1))
	if err := os.Symlink("missing.schema.json", filepath.Join(contracts, "events", "linked.schema.json")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	var problems diagnostics
	scanRepository(context.Background(), root, &problems)
	err := problems.err()
	assertErrorContains(t, err, "symlinks are forbidden")
	assertErrorContains(t, err, "file exceeds")
	assertErrorContains(t, err, "OpenAPI contracts must use .yaml or .yml")
}

func TestReadContractFileRechecksSize(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "large.json")
	mustWriteFile(t, path, make([]byte, maxContractSize+1))
	_, err := readContractFile(path)
	assertErrorContains(t, err, "file exceeds")
}

func findRepositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if fileExists(filepath.Join(directory, "go.mod")) && fileExists(filepath.Join(directory, "contracts")) {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatalf("repository root not found from %s", directory)
		}
		directory = parent
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatalf("create directory %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	// #nosec G703 -- This test helper only receives paths constructed beneath t.TempDir.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDiagnosticsLimit(t *testing.T) {
	t.Parallel()
	var problems diagnostics
	for index := 0; index < maxDiagnostics+5; index++ {
		problems.add("contract", "problem %d", index)
	}
	message := problems.err().Error()
	if !strings.Contains(message, "additional diagnostics omitted") {
		t.Fatalf("missing omission summary: %s", message)
	}
}

func TestDiagnosticsLimitIsOrderIndependent(t *testing.T) {
	t.Parallel()
	const total = maxDiagnostics + 500
	var forward diagnostics
	var reverse diagnostics
	for index := 0; index < total; index++ {
		forward.add("contract", "problem %04d", index)
	}
	for index := total - 1; index >= 0; index-- {
		reverse.add("contract", "problem %04d", index)
		reverse.add("contract", "problem %04d", index)
	}
	if got, want := reverse.err().Error(), forward.err().Error(); got != want {
		t.Fatalf("bounded diagnostics depend on insertion order:\nreverse: %s\nforward: %s", got, want)
	}
}

func TestGovernanceReviewInstancesMustSatisfyTheirContract(t *testing.T) {
	t.Parallel()
	repositoryRoot := findRepositoryRoot(t)
	var setup diagnostics
	files := scanRepository(context.Background(), repositoryRoot, &setup)
	parsed := checkJSONSyntax(context.Background(), files.jsonFiles, &setup)
	schemas := checkJSONSchemas(context.Background(), files.schemaFiles, parsed, &setup)
	if err := setup.err(); err != nil {
		t.Fatalf("compile governance schema: %v", err)
	}

	root := t.TempDir()
	reviews := filepath.Join(root, "architecture", "reviews")
	mustMkdirAll(t, reviews)
	mustWriteFile(t, filepath.Join(reviews, "080-invalid.json"), []byte(`{"schema_version":1}`))
	var problems diagnostics
	checkGovernanceInstances(context.Background(), root, schemas, &problems)
	assertErrorContains(t, problems.err(), "does not satisfy governance/architecture-review.schema.json")
}

func TestGovernanceReviewFilenameAllowsCanonicalStages(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"080-architecture-freeze.json", "088a-upload-foundation.json", "088b-upload-pipeline.json"} {
		if !architectureReviewName.MatchString(name) {
			t.Fatalf("canonical architecture review filename %q was rejected", name)
		}
	}
	for _, name := range []string{"88a-short.json", "088c-unknown-stage.json", "088A-uppercase-stage.json"} {
		if architectureReviewName.MatchString(name) {
			t.Fatalf("invalid architecture review filename %q was accepted", name)
		}
	}
}
