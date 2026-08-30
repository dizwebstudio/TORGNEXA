package marking

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func testPackage(id string, kind PackageKind) MarkingPackage {
	now := time.Now().UTC()
	return MarkingPackage{ID: id, OrganizationID: "org-1", WorkspaceID: "ws-1", Kind: kind, CodeFingerprint: strings.Repeat("a", 64), Status: "open", Version: 1, CreatedAt: now, UpdatedAt: now}
}

func TestCodeFingerprintIsStableAndDoesNotReturnRawValue(t *testing.T) {
	fingerprint, err := CodeFingerprint("010460123456789021ABC123")
	if err != nil || len(fingerprint) != 64 {
		t.Fatalf("fingerprint = %q, err = %v", fingerprint, err)
	}
	if strings.Contains(fingerprint, "010460") {
		t.Fatalf("fingerprint exposes raw code: %s", fingerprint)
	}
	if _, err := CodeFingerprint("bad\ncode"); !errors.Is(err, ErrRawCode) {
		t.Fatalf("newline must be rejected as raw code input, got %v", err)
	}
}

func TestPackageTreeRejectsCycle(t *testing.T) {
	packages := []MarkingPackage{testPackage("unit-1", PackageUnit), testPackage("box-1", PackageBox), testPackage("pallet-1", PackagePallet)}
	links := []PackageLink{{ParentID: "box-1", ChildID: "unit-1", Quantity: 1}, {ParentID: "pallet-1", ChildID: "box-1", Quantity: 1}}
	if err := ValidatePackageTree(packages, links); err != nil {
		t.Fatalf("valid tree rejected: %v", err)
	}
	links[0] = PackageLink{ParentID: "unit-1", ChildID: "box-1", Quantity: 1}
	if err := ValidatePackageTree(packages, links); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid unit parent not rejected: %v", err)
	}

	cycle := []MarkingPackage{testPackage("box-a", PackageBox), testPackage("box-b", PackageBox)}
	cycleLinks := []PackageLink{{ParentID: "box-a", ChildID: "box-b", Quantity: 1}, {ParentID: "box-b", ChildID: "box-a", Quantity: 1}}
	if err := ValidatePackageTree(cycle, cycleLinks); !errors.Is(err, ErrCycle) {
		t.Fatalf("cycle not rejected: %v", err)
	}
}

func TestCodeLifecyclePreservesUnknownRecovery(t *testing.T) {
	if !CanTransition(CodePrinted, CodeUnknown) || !CanTransition(CodeUnknown, CodeApplied) {
		t.Fatal("unknown lifecycle recovery is not allowed")
	}
	if CanTransition(CodeSold, CodeIntroduced) {
		t.Fatal("sold code cannot be silently reintroduced")
	}
}

func TestRawCodeHandleExpires(t *testing.T) {
	now := time.Now().UTC()
	handle := RawCodeHandle{ArtifactRef: "artifact:codes-1", Digest: strings.Repeat("b", 64), ExpiresAt: now.Add(time.Minute)}
	if err := handle.Validate(now); err != nil {
		t.Fatalf("valid handle rejected: %v", err)
	}
	if err := handle.Validate(now.Add(2 * time.Minute)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expired handle accepted: %v", err)
	}
}
