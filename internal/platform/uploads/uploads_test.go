package uploads

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const (
	org  = "018f25a0-7b11-7abc-8def-0123456789ab"
	ws   = "018f25a0-7b12-7abc-8def-0123456789ab"
	org2 = "018f25a0-7b13-7abc-8def-0123456789ab"
	ws2  = "018f25a0-7b14-7abc-8def-0123456789ab"
)

func mustScope(t *testing.T, o, w string) tenancy.Scope {
	t.Helper()
	scope, err := tenancy.ParseScope(o, w)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

type fakeRepo struct {
	record  Record
	created bool
	marked  bool
	getErr  error
}

func (r *fakeRepo) CreateReceived(_ context.Context, scope tenancy.Scope, record Record) error {
	if r.created || record.State != StateReceived || record.Validate(scope, DefaultMaxFileBytes) != nil {
		return ErrConflict
	}
	r.created = true
	r.record = record
	return nil
}
func (r *fakeRepo) MarkQuarantined(_ context.Context, scope tenancy.Scope, id ID, object StoredObject, mutation Mutation) (Record, error) {
	if !r.created || id != r.record.ID || mutation.Validate() != nil || object.Key != QuarantineObjectKey(scope, id) {
		return Record{}, ErrInvalid
	}
	at := mutation.OccurredAt
	r.record.State = StateQuarantined
	r.record.QuarantineObjectKey = object.Key
	r.record.ContentSizeBytes = object.SizeBytes
	r.record.ContentSHA256 = object.SHA256
	r.record.QuarantinedAt = &at
	r.record.UpdatedAt = at
	r.record.Version++
	r.marked = true
	return r.record, nil
}
func (r *fakeRepo) Get(_ context.Context, scope tenancy.Scope, id ID) (Record, error) {
	if r.getErr != nil {
		return Record{}, r.getErr
	}
	if id != r.record.ID || r.record.OrganizationID != scope.OrganizationID() || r.record.WorkspaceID != scope.WorkspaceID() {
		return Record{}, ErrNotFound
	}
	return r.record, nil
}

type fakeStore struct {
	called bool
	badKey bool
	bytes  []byte
}

func (s *fakeStore) PutQuarantined(_ context.Context, scope tenancy.Scope, id ID, reader io.Reader, max int64) (StoredObject, error) {
	s.called = true
	key := QuarantineObjectKey(scope, id)
	data, err := io.ReadAll(io.LimitReader(reader, max+1))
	if err != nil {
		return StoredObject{}, err
	}
	if int64(len(data)) > max {
		return StoredObject{}, ErrStorage
	}
	s.bytes = data
	sum := sha256.Sum256(data)
	if s.badKey {
		key = "quarantine/other/object"
	}
	return StoredObject{Key: key, SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}, nil
}

func TestReceiveQuarantinesUnderServerDerivedTenantKey(t *testing.T) {
	scope := mustScope(t, org, ws)
	repo := &fakeRepo{}
	store := &fakeStore{}
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	service, err := newService(repo, store, DefaultPolicy(), func() time.Time { return now }, bytes.NewReader(bytes.Repeat([]byte{0x01}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	mutation := Mutation{EventID: "evt_upload_088a", OccurredAt: now, Source: "api", CorrelationID: "req_088a"}
	got, err := service.Receive(context.Background(), scope, Metadata{OriginalFilename: "../invoice.csv", DeclaredMediaType: "text/csv", DeclaredSizeBytes: 3}, strings.NewReader("a,b"), mutation)
	if err != nil {
		t.Fatal(err)
	}
	if !repo.created || !repo.marked || !store.called || got.State != StateQuarantined {
		t.Fatalf("unexpected lifecycle: %+v", got)
	}
	if want := QuarantineObjectKey(scope, got.ID); got.QuarantineObjectKey != want {
		t.Fatalf("key=%q want=%q", got.QuarantineObjectKey, want)
	}
	if strings.Contains(got.QuarantineObjectKey, "invoice") || strings.Contains(got.QuarantineObjectKey, "..") {
		t.Fatal("client filename influenced object key")
	}
	if got.ReleasedObjectKey != "" || got.SecurityEvidenceID != "" {
		t.Fatal("foundation unexpectedly released upload")
	}
}

func TestReceiveWithIDIsRetrySafeForSameMetadata(t *testing.T) {
	scope := mustScope(t, org, ws)
	repo := &fakeRepo{}
	store := &fakeStore{}
	service, err := NewService(repo, store, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	id := ID("upl_0123456789abcdef0123456789abcdef")
	metadata := Metadata{OriginalFilename: "retry.txt", DeclaredMediaType: "text/plain", DeclaredSizeBytes: 5}
	mutation := Mutation{EventID: "event.retry", OccurredAt: time.Now().UTC(), Source: "api"}
	first, err := service.ReceiveWithID(context.Background(), scope, id, metadata, strings.NewReader("hello"), mutation)
	if err != nil {
		t.Fatal(err)
	}
	store.called = false
	second, err := service.ReceiveWithID(context.Background(), scope, id, metadata, strings.NewReader("ignored retry body"), mutation)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || second.State != StateQuarantined || store.called {
		t.Fatalf("first=%+v second=%+v storage_called=%v", first, second, store.called)
	}
}

func TestReceiveRejectsStorageKeyEscapeAndSizeMismatch(t *testing.T) {
	scope := mustScope(t, org, ws)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		store    *fakeStore
		metadata Metadata
	}{
		"key_escape":    {&fakeStore{badKey: true}, Metadata{OriginalFilename: "a.csv", DeclaredSizeBytes: 3}},
		"size_mismatch": {&fakeStore{}, Metadata{OriginalFilename: "a.csv", DeclaredSizeBytes: 4}},
	} {
		t.Run(name, func(t *testing.T) {
			service, _ := newService(&fakeRepo{}, tc.store, DefaultPolicy(), func() time.Time { return now }, bytes.NewReader(bytes.Repeat([]byte{0x02}, 64)))
			_, err := service.Receive(context.Background(), scope, tc.metadata, strings.NewReader("abc"), Mutation{EventID: "evt_088a", OccurredAt: now, Source: "api"})
			if err == nil {
				t.Fatal("expected failure")
			}
		})
	}
}

func TestAccessGateFailsClosedUntilReleasedWithEvidence(t *testing.T) {
	scope := mustScope(t, org, ws)
	other := mustScope(t, org2, ws2)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	id := ID("upl_00112233445566778899aabbccddeeff")
	q := QuarantineObjectKey(scope, id)
	sum := strings.Repeat("a", 64)
	repo := &fakeRepo{record: Record{ID: id, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Metadata: Metadata{OriginalFilename: "a.csv", DeclaredSizeBytes: 3}, State: StateQuarantined, QuarantineObjectKey: q, ContentSizeBytes: 3, ContentSHA256: sum, Version: 2, ReceivedAt: now, QuarantinedAt: &now, UpdatedAt: now}}
	gate, _ := NewAccessGate(repo, DefaultPolicy())
	if _, err := gate.ResolveReleased(context.Background(), scope, id); !errors.Is(err, ErrNotReleased) {
		t.Fatalf("quarantine err=%v", err)
	}
	if _, err := gate.ResolveReleased(context.Background(), other, id); !errors.Is(err, ErrNotReleased) {
		t.Fatalf("cross tenant err=%v", err)
	}
	released := now.Add(time.Minute)
	repo.record.State = StateReleased
	repo.record.ReleasedObjectKey = ReleasedObjectKey(scope, id)
	repo.record.SecurityEvidenceID = "uev_11111111111111111111111111111111"
	repo.record.ReleasedAt = &released
	repo.record.UpdatedAt = released
	repo.record.Version = 6
	ref, err := gate.ResolveReleased(context.Background(), scope, id)
	if err != nil {
		t.Fatal(err)
	}
	if !ref.Valid() || ref.UploadID() != id || ref.ObjectKey() != ReleasedObjectKey(scope, id) || ref.SHA256() != sum {
		t.Fatalf("bad ref: %+v", ref)
	}
}

func TestFoundationRepositoryRemainsNarrowAfterSecurityCompletion(t *testing.T) {
	if !errors.Is(ErrSecurityPipelineIncomplete, ErrSecurityPipelineIncomplete) {
		t.Fatal("sentinel missing")
	}
	// Compile-time API shape is the important invariant: the shared foundation
	// Repository remains read/receive/quarantine-only. SecurityPipelineRepository
	// owns every post-quarantine transition, so consumers cannot acquire release
	// authority merely by depending on the foundation boundary.
	var _ Repository = (*fakeRepo)(nil)
}
