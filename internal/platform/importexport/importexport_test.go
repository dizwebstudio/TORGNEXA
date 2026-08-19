package importexport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/catalog"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

const (
	orgID = "018f0000-0000-7000-8000-000000000001"
	wsID  = "018f0000-0000-7000-8000-000000000002"
	uplID = "upl_0123456789abcdef0123456789abcdef"
)

type uploadRepo struct{ record uploads.Record }

func (r uploadRepo) CreateReceived(context.Context, tenancy.Scope, uploads.Record) error { return nil }
func (r uploadRepo) MarkQuarantined(context.Context, tenancy.Scope, uploads.ID, uploads.StoredObject, uploads.Mutation) (uploads.Record, error) {
	return uploads.Record{}, errors.New("not used")
}
func (r uploadRepo) Get(context.Context, tenancy.Scope, uploads.ID) (uploads.Record, error) {
	return r.record, nil
}

type memoryReader struct {
	data   []byte
	tamper bool
}

func (r *memoryReader) OpenReleased(_ context.Context, _ tenancy.Scope, _ uploads.ReleasedObjectRef) (io.ReadCloser, error) {
	d := r.data
	if r.tamper {
		d = append([]byte(nil), d...)
		if len(d) > 0 {
			d[0] ^= 1
		}
	}
	return io.NopCloser(bytes.NewReader(d)), nil
}

type memoryCatalog struct {
	products map[string]catalog.Product
	creates  int
}

func newMemoryCatalog() *memoryCatalog { return &memoryCatalog{products: map[string]catalog.Product{}} }
func (m *memoryCatalog) Product(_ context.Context, _ catalog.Scope, id catalog.ProductID) (catalog.Product, error) {
	p, ok := m.products[id.String()]
	if !ok {
		return catalog.Product{}, catalog.ErrNotFound
	}
	return p, nil
}
func (m *memoryCatalog) CreateProduct(_ context.Context, scope catalog.Scope, cmd catalog.CreateProduct, mutation catalog.Mutation) (catalog.Product, error) {
	if cmd.Validate() != nil || mutation.Validate() != nil {
		return catalog.Product{}, catalog.ErrInvalidRecord
	}
	if _, ok := m.products[cmd.ID.String()]; ok {
		return catalog.Product{}, catalog.ErrConflict
	}
	for _, p := range m.products {
		if p.Code == cmd.Code {
			return catalog.Product{}, catalog.ErrConflict
		}
	}
	m.creates++
	at := mutation.OccurredAt
	p := catalog.Product{ID: cmd.ID, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Code: cmd.Code, Title: cmd.Title, Description: cmd.Description, Status: catalog.StatusDraft, Version: 1, CreatedAt: at, UpdatedAt: at}
	m.products[cmd.ID.String()] = p
	return p, nil
}

func fixture(t *testing.T, data []byte) (tenancy.Scope, uploads.ReleasedObjectRef, *uploads.AccessGate) {
	t.Helper()
	scope, err := tenancy.ParseScope(orgID, wsID)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	q := now.Add(-time.Minute)
	rel := now
	rec := uploads.Record{ID: uploads.ID(uplID), OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Metadata: uploads.Metadata{OriginalFilename: "products.csv", DeclaredMediaType: "text/csv", DeclaredSizeBytes: int64(len(data))}, State: uploads.StateReleased, QuarantineObjectKey: uploads.QuarantineObjectKey(scope, uploads.ID(uplID)), ReleasedObjectKey: uploads.ReleasedObjectKey(scope, uploads.ID(uplID)), ContentSizeBytes: int64(len(data)), ContentSHA256: hex.EncodeToString(sum[:]), SecurityEvidenceID: "uev_11111111111111111111111111111111", Version: 5, ReceivedAt: now.Add(-2 * time.Minute), QuarantinedAt: &q, ReleasedAt: &rel, UpdatedAt: rel}
	gate, err := uploads.NewAccessGate(uploadRepo{record: rec}, uploads.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := gate.ResolveReleased(context.Background(), scope, uploads.ID(uplID))
	if err != nil {
		t.Fatal(err)
	}
	return scope, ref, gate
}
func csvMapping() Mapping {
	return Mapping{ID: "supplier.default", Version: 1, Format: FormatCSV, Fields: map[TargetField]string{FieldProductID: "id", FieldCode: "code", FieldTitle: "name", FieldDescription: "description"}}
}

func TestCSVPreviewCommitAndReplay(t *testing.T) {
	data := []byte("id,code,name,description\n018f0000-0000-7000-8000-000000000101,SKU-1,Alpha,First\n018f0000-0000-7000-8000-000000000102,SKU-2,Beta,Second\n")
	scope, ref, gate := fixture(t, data)
	cat := newMemoryCatalog()
	svc, err := New(&memoryReader{data: data}, gate, cat, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC) }
	prepared, err := svc.Preview(context.Background(), scope, ref, csvMapping())
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.Preview().Ready() || prepared.Preview().TotalRows != 2 || prepared.Preview().ValidRows != 2 {
		t.Fatalf("preview=%+v", prepared.Preview())
	}
	result, err := svc.Commit(context.Background(), scope, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if result.CreatedRows != 2 || result.FailedRows != 0 {
		t.Fatalf("result=%+v", result)
	}
	replay, err := svc.Commit(context.Background(), scope, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if replay.UnchangedRows != 2 || replay.CreatedRows != 0 || cat.creates != 2 {
		t.Fatalf("replay=%+v creates=%d", replay, cat.creates)
	}
}

func TestValidationBlocksCommit(t *testing.T) {
	data := []byte("id,code,name\n018f0000-0000-7000-8000-000000000101,SKU-1,Alpha\n018f0000-0000-7000-8000-000000000101,SKU-2,Beta\n")
	scope, ref, gate := fixture(t, data)
	m := csvMapping()
	delete(m.Fields, FieldDescription)
	svc, _ := New(&memoryReader{data: data}, gate, newMemoryCatalog(), DefaultPolicy())
	prepared, err := svc.Preview(context.Background(), scope, ref, m)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Preview().InvalidRows != 1 || prepared.Preview().Ready() {
		t.Fatalf("preview=%+v", prepared.Preview())
	}
	if _, err = svc.Commit(context.Background(), scope, prepared); !errors.Is(err, ErrNotReady) {
		t.Fatalf("err=%v", err)
	}
}

func TestTamperedReleasedBytesFailClosed(t *testing.T) {
	data := []byte("id,code,name\n018f0000-0000-7000-8000-000000000101,SKU-1,Alpha\n")
	scope, ref, gate := fixture(t, data)
	m := csvMapping()
	delete(m.Fields, FieldDescription)
	svc, _ := New(&memoryReader{data: data, tamper: true}, gate, newMemoryCatalog(), DefaultPolicy())
	if _, err := svc.Preview(context.Background(), scope, ref, m); !errors.Is(err, ErrNotReleased) {
		t.Fatalf("err=%v", err)
	}
}

func TestJSONPreview(t *testing.T) {
	data := []byte(`[{"pid":"018f0000-0000-7000-8000-000000000101","sku":"SKU-1","title":"Alpha"}]`)
	scope, ref, gate := fixture(t, data)
	m := Mapping{ID: "json.default", Version: 2, Format: FormatJSON, Fields: map[TargetField]string{FieldProductID: "pid", FieldCode: "sku", FieldTitle: "title"}}
	svc, _ := New(&memoryReader{data: data}, gate, newMemoryCatalog(), DefaultPolicy())
	p, err := svc.Preview(context.Background(), scope, ref, m)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Preview().Ready() {
		t.Fatalf("preview=%+v", p.Preview())
	}
}

func TestExportProducts(t *testing.T) {
	at := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	id, _ := catalog.ParseProductID("018f0000-0000-7000-8000-000000000101")
	p := catalog.Product{ID: id, OrganizationID: orgID, WorkspaceID: wsID, Code: "SKU-1", Title: "Alpha", Description: "D", Status: catalog.StatusDraft, Version: 1, CreatedAt: at, UpdatedAt: at}
	csvOut, err := EncodeProducts(FormatCSV, []catalog.Product{p})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(csvOut, []byte("product_id,code,title")) {
		t.Fatalf("csv=%s", csvOut)
	}
	jsonOut, err := EncodeProducts(FormatJSON, []catalog.Product{p})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(jsonOut, []byte(`"product_id"`)) {
		t.Fatalf("json=%s", jsonOut)
	}
}

func TestMappingFingerprintStable(t *testing.T) {
	a := csvMapping()
	b := csvMapping()
	if a.Fingerprint() == "" || a.Fingerprint() != b.Fingerprint() {
		t.Fatal("unstable mapping fingerprint")
	}
	b.Version++
	if a.Fingerprint() == b.Fingerprint() {
		t.Fatal("version not bound")
	}
}

func TestQuarantinedObjectCannotYieldImportReference(t *testing.T) {
	data := []byte("x")
	scope, _ := tenancy.ParseScope(orgID, wsID)
	sum := sha256.Sum256(data)
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	q := now
	rec := uploads.Record{ID: uploads.ID(uplID), OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Metadata: uploads.Metadata{OriginalFilename: "x.csv", DeclaredSizeBytes: 1}, State: uploads.StateQuarantined, QuarantineObjectKey: uploads.QuarantineObjectKey(scope, uploads.ID(uplID)), ContentSizeBytes: 1, ContentSHA256: hex.EncodeToString(sum[:]), Version: 2, ReceivedAt: now.Add(-time.Minute), QuarantinedAt: &q, UpdatedAt: now}
	gate, _ := uploads.NewAccessGate(uploadRepo{record: rec}, uploads.DefaultPolicy())
	if _, err := gate.ResolveReleased(context.Background(), scope, uploads.ID(uplID)); !errors.Is(err, uploads.ErrNotReleased) {
		t.Fatalf("err=%v", err)
	}
}

type mutableUploadRepo struct{ record uploads.Record }

func (r *mutableUploadRepo) CreateReceived(context.Context, tenancy.Scope, uploads.Record) error {
	return nil
}
func (r *mutableUploadRepo) MarkQuarantined(context.Context, tenancy.Scope, uploads.ID, uploads.StoredObject, uploads.Mutation) (uploads.Record, error) {
	return uploads.Record{}, errors.New("not used")
}
func (r *mutableUploadRepo) Get(context.Context, tenancy.Scope, uploads.ID) (uploads.Record, error) {
	return r.record, nil
}

func TestStaleReleasedReferenceFailsBeforeImportRead(t *testing.T) {
	data := []byte("id,code,name\n018f0000-0000-7000-8000-000000000101,SKU-1,Alpha\n")
	scope, _ := tenancy.ParseScope(orgID, wsID)
	sum := sha256.Sum256(data)
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	q, rel := now.Add(-time.Minute), now
	repo := &mutableUploadRepo{record: uploads.Record{ID: uploads.ID(uplID), OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Metadata: uploads.Metadata{OriginalFilename: "products.csv", DeclaredMediaType: "text/csv", DeclaredSizeBytes: int64(len(data))}, State: uploads.StateReleased, QuarantineObjectKey: uploads.QuarantineObjectKey(scope, uploads.ID(uplID)), ReleasedObjectKey: uploads.ReleasedObjectKey(scope, uploads.ID(uplID)), ContentSizeBytes: int64(len(data)), ContentSHA256: hex.EncodeToString(sum[:]), SecurityEvidenceID: "uev_11111111111111111111111111111111", Version: 5, ReceivedAt: now.Add(-2 * time.Minute), QuarantinedAt: &q, ReleasedAt: &rel, UpdatedAt: rel}}
	gate, err := uploads.NewAccessGate(repo, uploads.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := gate.ResolveReleased(context.Background(), scope, uploads.ID(uplID))
	if err != nil {
		t.Fatal(err)
	}

	// A re-scan revokes the released capability before any scanner work begins.
	repo.record.State = uploads.StateQuarantined
	repo.record.ReleasedObjectKey = ""
	repo.record.ReleasedAt = nil
	repo.record.SecurityEvidenceID = ""
	repo.record.Version++
	repo.record.UpdatedAt = now.Add(time.Second)

	reader := &memoryReader{data: data}
	svc, err := New(reader, gate, newMemoryCatalog(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Preview(context.Background(), scope, ref, Mapping{ID: "supplier.default", Version: 1, Format: FormatCSV, Fields: map[TargetField]string{FieldProductID: "id", FieldCode: "code", FieldTitle: "name"}}); !errors.Is(err, ErrNotReleased) {
		t.Fatalf("stale release ref must fail closed before read: %v", err)
	}
}
