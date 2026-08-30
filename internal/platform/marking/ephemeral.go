package marking

import (
	"errors"
	"sync"
	"time"

	coremarking "github.com/torgnexa/torgnexa/internal/core/marking"
)

var ErrEphemeralUnavailable = errors.New("marking: ephemeral artifact unavailable")

type ephemeralRecord struct {
	value     []byte
	digest    string
	expiresAt time.Time
}

// EphemeralStore is a process-local protected contour for raw code material.
// It is intentionally not a general secret repository: values have a short
// TTL, are copied only into a callback, and are zeroed on release/expiry.
type EphemeralStore struct {
	mu      sync.Mutex
	records map[string]ephemeralRecord
	now     func() time.Time
	seq     uint64
}

// NewEphemeralStore constructs the short-lived raw-code store.
func NewEphemeralStore(clock func() time.Time) *EphemeralStore {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &EphemeralStore{records: make(map[string]ephemeralRecord), now: clock}
}

// Put stores raw material only in memory and returns a safe artifact handle.
func (s *EphemeralStore) Put(value []byte, ttl time.Duration) (coremarking.RawCodeHandle, error) {
	if s == nil || len(value) == 0 || len(value) > 1<<20 || ttl <= 0 || ttl > 15*time.Minute {
		return coremarking.RawCodeHandle{}, ErrEphemeralUnavailable
	}
	digest, err := coremarking.CodeFingerprint(string(value))
	if err != nil {
		return coremarking.RawCodeHandle{}, err
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	s.seq++
	ref := "artifact:marking-raw-" + formatSequence(s.seq)
	stored := append([]byte(nil), value...)
	expiresAt := now.Add(ttl)
	s.records[ref] = ephemeralRecord{value: stored, digest: digest, expiresAt: expiresAt}
	return coremarking.RawCodeHandle{ArtifactRef: ref, Digest: digest, ExpiresAt: expiresAt}, nil
}

// Use opens raw material only for the duration of fn and wipes its callback
// copy immediately afterwards.
func (s *EphemeralStore) Use(handle coremarking.RawCodeHandle, fn func([]byte) error) error {
	if s == nil || fn == nil {
		return ErrEphemeralUnavailable
	}
	now := s.now().UTC()
	if handle.Validate(now) != nil {
		return ErrEphemeralUnavailable
	}
	s.mu.Lock()
	record, ok := s.records[handle.ArtifactRef]
	if !ok || record.digest != handle.Digest || !record.expiresAt.After(now) {
		if ok {
			zero(record.value)
			delete(s.records, handle.ArtifactRef)
		}
		s.mu.Unlock()
		return ErrEphemeralUnavailable
	}
	copyValue := append([]byte(nil), record.value...)
	s.mu.Unlock()
	defer zero(copyValue)
	return fn(copyValue)
}

// Delete removes raw material and wipes the stored bytes.
func (s *EphemeralStore) Delete(handle coremarking.RawCodeHandle) error {
	if s == nil || handle.ArtifactRef == "" {
		return ErrEphemeralUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[handle.ArtifactRef]
	if !ok {
		return ErrEphemeralUnavailable
	}
	zero(record.value)
	delete(s.records, handle.ArtifactRef)
	return nil
}

func (s *EphemeralStore) cleanupLocked(now time.Time) {
	for ref, record := range s.records {
		if !record.expiresAt.After(now) {
			zero(record.value)
			delete(s.records, ref)
		}
	}
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func formatSequence(value uint64) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	if value == 0 {
		return "0"
	}
	var result [13]byte
	index := len(result)
	for value > 0 {
		index--
		result[index] = alphabet[value%uint64(len(alphabet))]
		value /= uint64(len(alphabet))
	}
	return string(result[index:])
}

var _ coremarking.RawCodeStore = (*EphemeralStore)(nil)
