package domain

import "testing"

func TestVersionHasSafeDevelopmentDefault(t *testing.T) {
	if got, want := Version(), "0.1.0-dev"; got != want {
		t.Fatalf("Version() = %q, want %q", got, want)
	}
}
