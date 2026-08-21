package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

type fakeUploadRepository struct {
	records map[uploads.ID]uploads.Record
}

func (f *fakeUploadRepository) CreateReceived(context.Context, tenancy.Scope, uploads.Record) error {
	return nil
}
func (f *fakeUploadRepository) MarkQuarantined(context.Context, tenancy.Scope, uploads.ID, uploads.StoredObject, uploads.Mutation) (uploads.Record, error) {
	return uploads.Record{}, nil
}
func (f *fakeUploadRepository) Get(_ context.Context, scope tenancy.Scope, id uploads.ID) (uploads.Record, error) {
	record, ok := f.records[id]
	if !ok || record.OrganizationID != scope.OrganizationID() || record.WorkspaceID != scope.WorkspaceID() {
		return uploads.Record{}, uploads.ErrNotFound
	}
	return record, nil
}

type fakeReleaseReader struct {
	key     string
	payload []byte
	err     error
}

func (f *fakeReleaseReader) OpenReleased(_ context.Context, _ tenancy.Scope, _ uploads.ID, key string) (uploads.ReleasedObject, error) {
	if f.err != nil {
		return nil, f.err
	}
	if key != f.key {
		return nil, uploads.ErrNotFound
	}
	return io.NopCloser(strings.NewReader(string(f.payload))), nil
}

func releasedTestRecord(t *testing.T, scope tenancy.Scope, id uploads.ID) uploads.Record {
	t.Helper()
	now := time.Now().UTC()
	quarantinedAt := now.Add(-time.Minute)
	record := uploads.Record{
		ID: id, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(),
		Metadata:            uploads.Metadata{OriginalFilename: "logo.png", DeclaredSizeBytes: 4},
		State:               uploads.StateReleased,
		QuarantineObjectKey: uploads.QuarantineObjectKey(scope, id),
		ReleasedObjectKey:   uploads.ReleasedObjectKey(scope, id),
		ContentSizeBytes:    4,
		ContentSHA256:       strings.Repeat("a", 64),
		SecurityEvidenceID:  "uev_" + strings.Repeat("b", 32),
		Version:             3,
		ReceivedAt:          quarantinedAt.Add(-time.Minute),
		QuarantinedAt:       &quarantinedAt,
		ReleasedAt:          &now,
		UpdatedAt:           now,
	}
	if err := record.Validate(scope, uploads.DefaultPolicy().MaxFileBytes); err != nil {
		t.Fatalf("test fixture is not a valid released record: %v", err)
	}
	return record
}

func TestGetUploadStatusReturnsLifecycleStateInTenantScope(t *testing.T) {
	scope := validTestScope(t)
	id := uploads.ID("upl_" + strings.Repeat("1", 32))
	repo := &fakeUploadRepository{records: map[uploads.ID]uploads.Record{id: releasedTestRecord(t, scope, id)}}
	gate, err := uploads.NewAccessGate(repo, uploads.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	content := &fakeReleaseReader{key: uploads.ReleasedObjectKey(scope, id), payload: []byte("PNG!")}
	routes := newUploadReadRoutes(repo, gate, content)
	if len(routes) != 1 {
		t.Fatalf("routes=%d", len(routes))
	}

	request := productionRequestContext(t, httptest.NewRequest(http.MethodGet, "/api/v1/uploads/"+string(id), nil))
	response := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"released"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGetUploadStatusFailsClosedAcrossTenants(t *testing.T) {
	other, err := tenancy.ParseScope("018f1c8a-7b3c-7def-8000-0000000000ff", "018f1c8a-7b3c-7def-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	id := uploads.ID("upl_" + strings.Repeat("2", 32))
	repo := &fakeUploadRepository{records: map[uploads.ID]uploads.Record{id: releasedTestRecord(t, other, id)}}
	gate, err := uploads.NewAccessGate(repo, uploads.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	routes := newUploadReadRoutes(repo, gate, &fakeReleaseReader{})

	request := productionRequestContext(t, httptest.NewRequest(http.MethodGet, "/api/v1/uploads/"+string(id), nil))
	response := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGetUploadContentServesReleasedBytesWithSniffedType(t *testing.T) {
	scope := validTestScope(t)
	id := uploads.ID("upl_" + strings.Repeat("3", 32))
	repo := &fakeUploadRepository{records: map[uploads.ID]uploads.Record{id: releasedTestRecord(t, scope, id)}}
	gate, err := uploads.NewAccessGate(repo, uploads.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\n" + "rest-of-file")
	content := &fakeReleaseReader{key: uploads.ReleasedObjectKey(scope, id), payload: png}
	routes := newUploadReadRoutes(repo, gate, content)

	request := productionRequestContext(t, httptest.NewRequest(http.MethodGet, "/api/v1/uploads/"+string(id)+"/content", nil))
	response := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != string(png) || response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("status=%d contentType=%s body=%q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}

func TestGetUploadContentRejectsUnreleasedUpload(t *testing.T) {
	scope := validTestScope(t)
	id := uploads.ID("upl_" + strings.Repeat("4", 32))
	quarantinedAt := time.Now().UTC()
	record := uploads.Record{
		ID: id, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(),
		Metadata: uploads.Metadata{OriginalFilename: "logo.png", DeclaredSizeBytes: 4}, State: uploads.StateScanning,
		QuarantineObjectKey: uploads.QuarantineObjectKey(scope, id), ContentSizeBytes: 4, ContentSHA256: strings.Repeat("a", 64),
		ReceivedAt: quarantinedAt.Add(-time.Minute), QuarantinedAt: &quarantinedAt, UpdatedAt: quarantinedAt,
	}
	repo := &fakeUploadRepository{records: map[uploads.ID]uploads.Record{id: record}}
	gate, err := uploads.NewAccessGate(repo, uploads.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	routes := newUploadReadRoutes(repo, gate, &fakeReleaseReader{})

	request := productionRequestContext(t, httptest.NewRequest(http.MethodGet, "/api/v1/uploads/"+string(id)+"/content", nil))
	response := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestUploadReadRouteRejectsUnknownSuffix(t *testing.T) {
	scope := validTestScope(t)
	id := uploads.ID("upl_" + strings.Repeat("5", 32))
	repo := &fakeUploadRepository{records: map[uploads.ID]uploads.Record{id: releasedTestRecord(t, scope, id)}}
	gate, err := uploads.NewAccessGate(repo, uploads.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	routes := newUploadReadRoutes(repo, gate, &fakeReleaseReader{})

	request := productionRequestContext(t, httptest.NewRequest(http.MethodGet, "/api/v1/uploads/"+string(id)+"/metadata", nil))
	response := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d", response.Code)
	}
}
