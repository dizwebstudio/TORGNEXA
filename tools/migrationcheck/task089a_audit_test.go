package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

var task089aFloatPattern = regexp.MustCompile(`(?m)\bfloat(?:32|64)\b`)

// Stage 089a remains the immutable exact contract foundation even after stage
// 089b enables the separately reviewed runtime/storage implementation.
func TestTask089aFoundationRemainsExact(t *testing.T) {
	root := task089aRepositoryRoot(t)
	for _, relative := range []string{
		"internal/platform/fx/fx.go",
		"internal/platform/fx/fx_test.go",
	} {
		data := task089aRead(t, root, relative)
		if task089aFloatPattern.Match(data) {
			t.Fatalf("%s contains binary floating-point FX type", relative)
		}
	}
}

func TestTask089aPreservesPublishedV1FXContracts(t *testing.T) {
	root := task089aRepositoryRoot(t)
	expected := map[string]string{
		"contracts/fx/rate.schema.json":                     "56c72fe73a09a94ace010f2f4611daef630c503f232a83124c1666daaab8734f",
		"contracts/events/fx-rate-published-v1.schema.json": "00fa8db5c32dda00ea98f8f6ad63f109f4bb6f472d2802f2fd3664d143dcf23e",
	}
	for relative, want := range expected {
		sum := sha256.Sum256(task089aRead(t, root, relative))
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Fatalf("published v1 FX contract %s changed: got %s want %s", relative, got, want)
		}
	}
}

func task089aRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve Task-089a audit source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}

func task089aRead(t *testing.T, root, relative string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return data
}
