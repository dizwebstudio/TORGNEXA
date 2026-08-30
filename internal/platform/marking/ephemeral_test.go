package marking

import (
	"errors"
	"testing"
	"time"
)

func TestEphemeralStoreScopesRawCodeToCallbackAndTTL(t *testing.T) {
	now := time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC)
	store := NewEphemeralStore(func() time.Time { return now })
	handle, err := store.Put([]byte("010460123456789021ABC123"), time.Minute)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	called := false
	if err := store.Use(handle, func(value []byte) error { called = string(value) == "010460123456789021ABC123"; return nil }); err != nil || !called {
		t.Fatalf("use: err=%v called=%v", err, called)
	}
	now = now.Add(2 * time.Minute)
	if err := store.Use(handle, func([]byte) error { return nil }); !errors.Is(err, ErrEphemeralUnavailable) {
		t.Fatalf("expired artifact was usable: %v", err)
	}
}
