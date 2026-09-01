package tenancy

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

const (
	testOrganizationID = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	testWorkspaceID    = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
	testStoreID        = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003"
)

func TestSortableIDParsing(t *testing.T) {
	t.Parallel()
	valid := []string{
		testOrganizationID,
		"01ARZ3NDEKTSV4RRFFQ69G5FAV",
		"7ZZZZZZZZZZZZZZZZZZZZZZZZZ",
	}
	for _, value := range valid {
		if _, err := ParseOrganizationID(value); err != nil {
			t.Errorf("ParseOrganizationID(%q) error = %v", value, err)
		}
	}
	invalid := []string{
		"",
		"018f0e8b-8a58-4f42-8c2d-5c2f9b1a0001",
		"018F0E8B-8A58-7F42-8C2D-5C2F9B1A0001",
		"01ARZ3NDEKTSV4RRFFQ69G5FAI",
		"8ZZZZZZZZZZZZZZZZZZZZZZZZZ",
	}
	for _, value := range invalid {
		if _, err := ParseOrganizationID(value); !errors.Is(err, ErrInvalidID) {
			t.Errorf("ParseOrganizationID(%q) error = %v, want ErrInvalidID", value, err)
		}
	}
}

func TestUUIDv7Generation(t *testing.T) {
	t.Parallel()
	random := bytes.NewReader(bytes.Repeat([]byte{0xa5}, 20))
	first, err := newUUIDv7(time.UnixMilli(1_700_000_000_000), random)
	if err != nil {
		t.Fatalf("newUUIDv7() error = %v", err)
	}
	second, err := newUUIDv7(time.UnixMilli(1_700_000_000_001), random)
	if err != nil {
		t.Fatalf("newUUIDv7() second error = %v", err)
	}
	if !domain.ValidUUIDv7(first) || first[14] != '7' || !strings.Contains("89ab", first[19:20]) {
		t.Fatalf("newUUIDv7() = %q, want canonical version/variant", first)
	}
	if first >= second {
		t.Fatalf("UUIDv7 timestamp ordering lost: %q >= %q", first, second)
	}
}

func TestUUIDv7GenerationFailures(t *testing.T) {
	t.Parallel()
	if _, err := newUUIDv7(time.UnixMilli(-1), bytes.NewReader(make([]byte, 10))); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("negative timestamp error = %v", err)
	}
	if _, err := newUUIDv7(time.UnixMilli(1), bytes.NewReader(nil)); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("short randomness error = %v", err)
	}
}

func TestScopeRequiresBothCanonicalIDs(t *testing.T) {
	t.Parallel()
	organization, _ := ParseOrganizationID(testOrganizationID)
	workspace, _ := ParseWorkspaceID(testWorkspaceID)
	scope, err := NewScope(organization, workspace)
	if err != nil || !scope.Valid() || scope.OrganizationID() != organization || scope.WorkspaceID() != workspace {
		t.Fatalf("NewScope() = %#v, %v", scope, err)
	}
	if _, err := NewScope(organization, ""); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("NewScope() error = %v, want ErrInvalidScope", err)
	}
	if _, err := ParseScope("invalid", testWorkspaceID); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("ParseScope() error = %v, want ErrInvalidScope", err)
	}
}
