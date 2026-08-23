package uploads

import (
	"archive/zip"
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

type pipelineRepo struct {
	record   Record
	evidence []SecurityEvidence
}

func (r *pipelineRepo) CreateReceived(context.Context, tenancy.Scope, Record) error {
	return ErrConflict
}
func (r *pipelineRepo) MarkQuarantined(context.Context, tenancy.Scope, ID, StoredObject, Mutation) (Record, error) {
	return Record{}, ErrConflict
}
func (r *pipelineRepo) Get(_ context.Context, scope tenancy.Scope, id ID) (Record, error) {
	if id != r.record.ID || r.record.OrganizationID != scope.OrganizationID() || r.record.WorkspaceID != scope.WorkspaceID() {
		return Record{}, ErrNotFound
	}
	return r.record, nil
}
func (r *pipelineRepo) MarkValidated(_ context.Context, scope tenancy.Scope, id ID, at time.Time) (Record, error) {
	if r.record.State != StateQuarantined || id != r.record.ID {
		return Record{}, ErrConflict
	}
	r.record.State = StateValidated
	r.record.Version++
	r.record.UpdatedAt = at
	if r.record.Validate(scope, DefaultMaxFileBytes) != nil {
		return Record{}, ErrInvalid
	}
	return r.record, nil
}
func (r *pipelineRepo) MarkScanning(_ context.Context, scope tenancy.Scope, id ID, at time.Time) (Record, error) {
	if r.record.State != StateValidated || id != r.record.ID {
		return Record{}, ErrConflict
	}
	r.record.State = StateScanning
	r.record.Version++
	r.record.UpdatedAt = at
	if r.record.Validate(scope, DefaultMaxFileBytes) != nil {
		return Record{}, ErrInvalid
	}
	return r.record, nil
}
func (r *pipelineRepo) RecordDecision(_ context.Context, scope tenancy.Scope, id ID, e SecurityEvidence, m Mutation) (Record, SecurityEvidence, error) {
	if id != r.record.ID || m.Validate() != nil {
		return Record{}, SecurityEvidence{}, ErrInvalid
	}
	e.Attempt = int64(len(r.evidence) + 1)
	if e.Validate(scope, DefaultMaxFileBytes) != nil {
		return Record{}, SecurityEvidence{}, ErrInvalid
	}
	allowed := (e.Decision == DecisionError && r.record.State == StateScanning) || (e.Decision == DecisionClean && r.record.State == StateScanning) || (e.Decision == DecisionRejected && (r.record.State == StateScanning || r.record.State == StateQuarantined))
	if !allowed {
		return Record{}, SecurityEvidence{}, ErrConflict
	}
	r.evidence = append(r.evidence, e)
	if e.Decision != DecisionError {
		if e.Decision == DecisionClean {
			r.record.State = StateClean
		} else {
			r.record.State = StateRejected
		}
		r.record.SecurityEvidenceID = e.ID
		r.record.Version++
		r.record.UpdatedAt = m.OccurredAt
	}
	return r.record, e, nil
}
func (r *pipelineRepo) MarkReleased(_ context.Context, scope tenancy.Scope, id ID, evidenceID string, object StoredObject, m Mutation) (Record, error) {
	if r.record.State != StateClean || evidenceID != r.record.SecurityEvidenceID || object.Key != ReleasedObjectKey(scope, id) || object.SHA256 != r.record.ContentSHA256 || object.SizeBytes != r.record.ContentSizeBytes {
		return Record{}, ErrConflict
	}
	r.record.State = StateReleased
	r.record.ReleasedObjectKey = object.Key
	at := m.OccurredAt
	r.record.ReleasedAt = &at
	r.record.UpdatedAt = at
	r.record.Version++
	if r.record.Validate(scope, DefaultMaxFileBytes) != nil {
		return Record{}, ErrInvalid
	}
	return r.record, nil
}
func (r *pipelineRepo) RequestRescan(_ context.Context, scope tenancy.Scope, id ID, reason string, m Mutation) (Record, error) {
	if id != r.record.ID || !machineCodePattern.MatchString(reason) || (r.record.State != StateClean && r.record.State != StateRejected && r.record.State != StateReleased) {
		return Record{}, ErrConflict
	}
	r.record.State = StateQuarantined
	r.record.ReleasedObjectKey = ""
	r.record.SecurityEvidenceID = ""
	r.record.ReleasedAt = nil
	r.record.UpdatedAt = m.OccurredAt
	r.record.Version++
	if r.record.Validate(scope, DefaultMaxFileBytes) != nil {
		return Record{}, ErrInvalid
	}
	return r.record, nil
}
func (r *pipelineRepo) ListSecurityEvidence(_ context.Context, scope tenancy.Scope, id ID, limit int) ([]SecurityEvidence, error) {
	if id != r.record.ID || limit < 1 {
		return nil, ErrInvalid
	}
	out := append([]SecurityEvidence(nil), r.evidence...)
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

type memorySecurityStore struct {
	quarantine map[string][]byte
	released   map[string][]byte
}
type memoryObject struct{ *bytes.Reader }

func (memoryObject) Close() error { return nil }
func (s *memorySecurityStore) OpenQuarantined(_ context.Context, _ tenancy.Scope, _ ID, key string) (QuarantinedObject, error) {
	data, ok := s.quarantine[key]
	if !ok {
		return nil, ErrStorage
	}
	return memoryObject{bytes.NewReader(data)}, nil
}
func (s *memorySecurityStore) Promote(_ context.Context, scope tenancy.Scope, id ID, fromKey, digest string) (StoredObject, error) {
	data, ok := s.quarantine[fromKey]
	if !ok {
		return StoredObject{}, ErrStorage
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != digest {
		return StoredObject{}, ErrStorage
	}
	key := ReleasedObjectKey(scope, id)
	if s.released == nil {
		s.released = map[string][]byte{}
	}
	s.released[key] = append([]byte(nil), data...)
	return StoredObject{Key: key, SizeBytes: int64(len(data)), SHA256: got}, nil
}

type fakeScanner struct {
	status ScannerStatus
	err    error
	calls  int
	threat string
}

func (s *fakeScanner) Scan(_ context.Context, req ScanRequest, r io.Reader) (ScanResult, error) {
	s.calls++
	data, err := io.ReadAll(r)
	if err != nil {
		return ScanResult{}, err
	}
	if int64(len(data)) != req.SizeBytes {
		return ScanResult{}, ErrInvalid
	}
	if s.err != nil {
		return ScanResult{ScannerName: "test_scanner", EngineVersion: "1.0", SignatureVersion: "sig-1", Status: ScannerError}, s.err
	}
	res := ScanResult{ScannerName: "test_scanner", EngineVersion: "1.0", SignatureVersion: "sig-1", Status: s.status, ThreatCode: s.threat}
	return res, nil
}

type lazyScanner struct{}

func (lazyScanner) Scan(context.Context, ScanRequest, io.Reader) (ScanResult, error) {
	return ScanResult{ScannerName: "lazy", EngineVersion: "1", SignatureVersion: "1", Status: ScannerClean}, nil
}

type metricSink struct{ observations []MetricObservation }

func (m *metricSink) ObserveUploadSecurity(v MetricObservation) {
	m.observations = append(m.observations, v)
}

func seedQuarantined(t *testing.T, filename, declared string, data []byte) (tenancy.Scope, *pipelineRepo, *memorySecurityStore, ID) {
	t.Helper()
	scope := mustScope(t, org, ws)
	id := ID("upl_102132435465768798a9bacbdcedfe0f")
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	sum := sha256.Sum256(data)
	q := now.Add(-time.Minute)
	r := Record{ID: id, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Metadata: Metadata{OriginalFilename: filename, DeclaredMediaType: declared, DeclaredSizeBytes: int64(len(data))}, State: StateQuarantined, QuarantineObjectKey: QuarantineObjectKey(scope, id), ContentSizeBytes: int64(len(data)), ContentSHA256: hex.EncodeToString(sum[:]), Version: 2, ReceivedAt: now.Add(-2 * time.Minute), QuarantinedAt: &q, UpdatedAt: q}
	if r.Validate(scope, DefaultMaxFileBytes) != nil {
		t.Fatal("invalid seed")
	}
	repo := &pipelineRepo{record: r}
	store := &memorySecurityStore{quarantine: map[string][]byte{r.QuarantineObjectKey: append([]byte(nil), data...)}, released: map[string][]byte{}}
	return scope, repo, store, id
}
func baseMutation() Mutation {
	return Mutation{EventID: "evt_upload_security_088b", OccurredAt: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC), Source: "security_pipeline", CorrelationID: "req_088b", ActorID: "system"}
}
func deterministicRandom() io.Reader {
	b := make([]byte, 4096)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return bytes.NewReader(b)
}

func TestPipelineReleasesCleanCSVWithImmutableEvidence(t *testing.T) {
	data := []byte("id,code,title\n1,A,Alpha\n")
	scope, repo, store, id := seedQuarantined(t, "products.csv", "text/csv", data)
	scanner := &fakeScanner{status: ScannerClean}
	metrics := &metricSink{}
	now := time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC)
	p, err := newPipeline(repo, store, store, scanner, metrics, DefaultPolicy(), func() time.Time { return now }, deterministicRandom())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := p.Process(context.Background(), scope, id, baseMutation())
	if err != nil {
		t.Fatal(err)
	}
	if repo.record.State != StateReleased || !ref.Valid() || ref.EvidenceID() == "" || ref.RecordVersion() != repo.record.Version || scanner.calls != 1 {
		t.Fatalf("record=%+v ref=%+v calls=%d", repo.record, ref, scanner.calls)
	}
	if len(repo.evidence) != 1 || repo.evidence[0].Decision != DecisionClean || repo.evidence[0].DetectedMediaType != "text/csv" || repo.evidence[0].Scanner.Status != ScannerClean {
		t.Fatalf("evidence=%+v", repo.evidence)
	}
	gate, _ := NewAccessGate(repo, DefaultPolicy())
	if err := gate.ValidateReleasedRef(context.Background(), scope, ref); err != nil {
		t.Fatal(err)
	}
	if len(metrics.observations) < 2 {
		t.Fatalf("metrics=%+v", metrics.observations)
	}
}

func TestPipelineRejectsFilenameTraversalAndMIMEMismatchBeforeScanner(t *testing.T) {
	cases := []struct {
		name, file, declared string
		data                 []byte
	}{
		{"path", "../products.csv", "text/csv", []byte("a,b\n1,2\n")},
		{"mime", "products.json", "application/json", []byte("a,b\n1,2\n")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope, repo, store, id := seedQuarantined(t, tc.file, tc.declared, tc.data)
			scanner := &fakeScanner{status: ScannerClean}
			p, _ := newPipeline(repo, store, store, scanner, nil, DefaultPolicy(), func() time.Time { return time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC) }, deterministicRandom())
			_, err := p.Process(context.Background(), scope, id, baseMutation())
			if !errors.Is(err, ErrSecurityRejected) {
				t.Fatalf("err=%v", err)
			}
			if scanner.calls != 0 || repo.record.State != StateRejected || len(repo.evidence) != 1 || repo.evidence[0].Scanner.Status != ScannerNotRun {
				t.Fatalf("calls=%d state=%s evidence=%+v", scanner.calls, repo.record.State, repo.evidence)
			}
		})
	}
}

func zipBytes(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func nestedZip(t *testing.T, depth int) []byte {
	data := []byte("safe")
	for i := 0; i < depth; i++ {
		data = zipBytes(t, map[string][]byte{"nested.zip": data})
	}
	return data
}

func TestPipelineRejectsArchiveTraversalBombAndDepth(t *testing.T) {
	bomb := bytes.Repeat([]byte("0"), 128*1024)
	cases := []struct {
		name   string
		data   []byte
		policy Policy
	}{
		{"traversal", zipBytes(t, map[string][]byte{"../escape.txt": []byte("x")}), DefaultPolicy()},
		{"ratio", zipBytes(t, map[string][]byte{"huge.txt": bomb}), func() Policy { p := DefaultPolicy(); p.MaxExpansionRatio = 2; return p }()},
		{"depth", nestedZip(t, 3), func() Policy { p := DefaultPolicy(); p.MaxArchiveDepth = 2; return p }()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope, repo, store, id := seedQuarantined(t, "bundle.zip", "application/zip", tc.data)
			scanner := &fakeScanner{status: ScannerClean}
			p, _ := newPipeline(repo, store, store, scanner, nil, tc.policy, func() time.Time { return time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC) }, deterministicRandom())
			_, err := p.Process(context.Background(), scope, id, baseMutation())
			if !errors.Is(err, ErrSecurityRejected) {
				t.Fatalf("err=%v", err)
			}
			if scanner.calls != 0 || repo.record.State != StateRejected {
				t.Fatalf("calls=%d state=%s", scanner.calls, repo.record.State)
			}
		})
	}
}

func TestScannerInfectionAndUnavailableAreFailClosed(t *testing.T) {
	data := []byte(`{"ok":true}`)
	t.Run("infected", func(t *testing.T) {
		scope, repo, store, id := seedQuarantined(t, "x.json", "application/json", data)
		scanner := &fakeScanner{status: ScannerInfected, threat: "synthetic_threat"}
		p, _ := newPipeline(repo, store, store, scanner, nil, DefaultPolicy(), func() time.Time { return time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC) }, deterministicRandom())
		_, err := p.Process(context.Background(), scope, id, baseMutation())
		if !errors.Is(err, ErrSecurityRejected) {
			t.Fatalf("err=%v", err)
		}
		if repo.record.State != StateRejected || repo.evidence[0].Scanner.Status != ScannerInfected {
			t.Fatalf("state=%s evidence=%+v", repo.record.State, repo.evidence)
		}
	})
	t.Run("scanner error status without transport error", func(t *testing.T) {
		scope, repo, store, id := seedQuarantined(t, "x.json", "application/json", data)
		scanner := &fakeScanner{status: ScannerError}
		p, _ := newPipeline(repo, store, store, scanner, nil, DefaultPolicy(), func() time.Time { return time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC) }, deterministicRandom())
		_, err := p.Process(context.Background(), scope, id, baseMutation())
		if !errors.Is(err, ErrScannerUnavailable) {
			t.Fatalf("err=%v", err)
		}
		if repo.record.State != StateScanning || len(repo.evidence) != 1 || repo.evidence[0].Decision != DecisionError {
			t.Fatalf("state=%s evidence=%+v", repo.record.State, repo.evidence)
		}
	})

	t.Run("retry", func(t *testing.T) {
		scope, repo, store, id := seedQuarantined(t, "x.json", "application/json", data)
		scanner := &fakeScanner{status: ScannerClean, err: ErrScannerUnavailable}
		p, _ := newPipeline(repo, store, store, scanner, nil, DefaultPolicy(), func() time.Time { return time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC) }, deterministicRandom())
		_, err := p.Process(context.Background(), scope, id, baseMutation())
		if !errors.Is(err, ErrScannerUnavailable) {
			t.Fatalf("err=%v", err)
		}
		if repo.record.State != StateScanning || len(repo.evidence) != 1 || repo.evidence[0].Decision != DecisionError {
			t.Fatalf("state=%s evidence=%+v", repo.record.State, repo.evidence)
		}
		scanner.err = nil
		ref, err := p.Process(context.Background(), scope, id, baseMutation())
		if err != nil {
			t.Fatal(err)
		}
		if !ref.Valid() || repo.record.State != StateReleased || len(repo.evidence) != 2 {
			t.Fatalf("state=%s evidence=%d", repo.record.State, len(repo.evidence))
		}
	})
}

func TestScannerMustConsumeEntireObjectBeforeCleanDecision(t *testing.T) {
	data := []byte("a,b\n1,2\n")
	scope, repo, store, id := seedQuarantined(t, "x.csv", "text/csv", data)
	p, _ := newPipeline(repo, store, store, lazyScanner{}, nil, DefaultPolicy(), func() time.Time { return time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC) }, deterministicRandom())
	_, err := p.Process(context.Background(), scope, id, baseMutation())
	if !errors.Is(err, ErrScannerUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if repo.record.State != StateScanning || len(repo.evidence) != 1 || repo.evidence[0].Decision != DecisionError {
		t.Fatalf("state=%s evidence=%+v", repo.record.State, repo.evidence)
	}
}

func TestRescanRevokesOldReleasedReferenceBeforeNewScan(t *testing.T) {
	data := []byte("a,b\n1,2\n")
	scope, repo, store, id := seedQuarantined(t, "x.csv", "text/csv", data)
	scanner := &fakeScanner{status: ScannerClean}
	now := time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC)
	p, _ := newPipeline(repo, store, store, scanner, nil, DefaultPolicy(), func() time.Time { return now }, deterministicRandom())
	oldRef, err := p.Process(context.Background(), scope, id, baseMutation())
	if err != nil {
		t.Fatal(err)
	}
	gate, _ := NewAccessGate(repo, DefaultPolicy())
	if err := gate.ValidateReleasedRef(context.Background(), scope, oldRef); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if _, err := p.RequestRescan(context.Background(), scope, id, "signature_update", baseMutation()); err != nil {
		t.Fatal(err)
	}
	if err := gate.ValidateReleasedRef(context.Background(), scope, oldRef); !errors.Is(err, ErrNotReleased) {
		t.Fatalf("old ref err=%v", err)
	}
	newRef, err := p.Process(context.Background(), scope, id, baseMutation())
	if err != nil {
		t.Fatal(err)
	}
	if newRef.EvidenceID() == oldRef.EvidenceID() || newRef.RecordVersion() <= oldRef.RecordVersion() {
		t.Fatalf("old=%+v new=%+v", oldRef, newRef)
	}
	if err := gate.ValidateReleasedRef(context.Background(), scope, newRef); err != nil {
		t.Fatal(err)
	}
}

func TestParserLimitsRejectDeepJSONAndXMLDoctype(t *testing.T) {
	policy := DefaultPolicy()
	policy.MaxParserDepth = 3
	for name, fixture := range map[string]struct {
		file, mime string
		data       []byte
	}{
		"json": {"x.json", "application/json", []byte(`[[[[1]]]]`)},
		"xml":  {"x.xml", "application/xml", []byte(`<!DOCTYPE x [<!ENTITY y "z">]><x>&y;</x>`)},
	} {
		t.Run(name, func(t *testing.T) {
			scope, repo, store, id := seedQuarantined(t, fixture.file, fixture.mime, fixture.data)
			scanner := &fakeScanner{status: ScannerClean}
			p, _ := newPipeline(repo, store, store, scanner, nil, policy, func() time.Time { return time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC) }, deterministicRandom())
			_, err := p.Process(context.Background(), scope, id, baseMutation())
			if !errors.Is(err, ErrSecurityRejected) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestSafeFilenameDoesNotTreatClientNameAsPath(t *testing.T) {
	for _, name := range []string{"a/b.csv", `a\\b.csv`, "..", ".", " x.csv"} {
		if safeUploadFilename(name) {
			t.Fatalf("accepted %q", name)
		}
	}
	if !safeUploadFilename("products-2026.csv") {
		t.Fatal("safe name rejected")
	}
	if normalizedExtension(strings.Repeat("a", 20)+".csv") != ".csv" {
		t.Fatal("extension parse")
	}
}

func TestExpansionRatioDoesNotTruncateFractionalOverflow(t *testing.T) {
	if expansionRatioExceeded(10000, 100, 100) {
		t.Fatal("exact policy ratio must pass")
	}
	if !expansionRatioExceeded(10001, 100, 100) {
		t.Fatal("fractional ratio above policy must fail")
	}
}

func TestArchiveSizeRejectsUint64OverflowBeforeConversion(t *testing.T) {
	if _, ok := archiveSizeWithinLimit(^uint64(0), DefaultPolicy().MaxArchiveEntryBytes); ok {
		t.Fatal("ZIP64 size above int64/policy limit must be rejected")
	}
	want := DefaultPolicy().MaxArchiveEntryBytes
	got, ok := archiveSizeWithinLimit(uint64(128*1024*1024), want)
	if !ok || got != want {
		t.Fatalf("boundary size got=%d ok=%v want=%d", got, ok, want)
	}
}

func TestScannerMetadataRejectsCredentialShapedText(t *testing.T) {
	result := ScanResult{
		ScannerName:      "clamav",
		EngineVersion:    "Bearer should-never-be-evidence",
		SignatureVersion: "sig-1",
		Status:           ScannerClean,
	}
	if !errors.Is(result.Validate(), ErrInvalid) {
		t.Fatal("credential-shaped scanner metadata must be rejected")
	}
}
