package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/legalparty"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/reconciliation"
	"github.com/torgnexa/torgnexa/internal/platform/retention"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

type contractRunStore struct {
	run reconciliation.Run
}

func (store *contractRunStore) CreateRun(_ context.Context, _ tenancy.Scope, run reconciliation.Run) (reconciliation.Run, error) {
	if store.run.ID != "" {
		return reconciliation.Run{}, errors.New("duplicate")
	}
	store.run = run
	return run, nil
}

func (store *contractRunStore) Run(_ context.Context, _ tenancy.Scope, id string) (reconciliation.Run, error) {
	if store.run.ID != id {
		return reconciliation.Run{}, reconciliation.ErrNotFound
	}
	return store.run, nil
}

type privacyWorkflowStub struct {
	spec retention.SubjectRequestSpec
}

type uploadReceiverStub struct {
	metadata uploads.Metadata
	content  string
}

type counterpartyListerStub struct {
	scope legalparty.Scope
}

func (stub *counterpartyListerStub) ListCounterparties(_ context.Context, scope legalparty.Scope, _ int) ([]legalparty.Counterparty, error) {
	stub.scope = scope
	now := time.Now().UTC()
	id, _ := legalparty.ParseID("0198b8d0-0000-7000-8000-000000000010")
	partyID, _ := legalparty.ParseID("0198b8d0-0000-7000-8000-000000000011")
	return []legalparty.Counterparty{{ID: id, Code: "supplier-1", PartyType: legalparty.PartyLegalEntity, PartyID: partyID, Role: legalparty.RoleSupplier, Status: legalparty.StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}}, nil
}

func (stub *uploadReceiverStub) ReceiveWithID(_ context.Context, _ tenancy.Scope, id uploads.ID, metadata uploads.Metadata, source io.Reader, _ uploads.Mutation) (uploads.Record, error) {
	content, _ := io.ReadAll(source)
	stub.metadata, stub.content = metadata, string(content)
	now := time.Now().UTC()
	return uploads.Record{ID: id, State: uploads.StateQuarantined, ContentSizeBytes: int64(len(content)), ContentSHA256: strings.Repeat("0", 64), Version: 2, ReceivedAt: now, QuarantinedAt: &now}, nil
}

func (stub *privacyWorkflowStub) CreateSubjectRequest(_ context.Context, _ tenancy.Scope, spec retention.SubjectRequestSpec) (retention.Job, error) {
	stub.spec = spec
	return retention.Job{ID: spec.JobID, RequestID: spec.RequestID, Action: retention.ActionDelete, Status: retention.StatusPending}, nil
}

func productionRequestContext(t *testing.T, request *http.Request) *http.Request {
	t.Helper()
	ctx := context.WithValue(request.Context(), requestScopeKey{}, validTestScope(t))
	ctx = context.WithValue(ctx, requestIdentityKey{}, Principal{Issuer: "https://id.example.test", Subject: "user|opaque"})
	return request.WithContext(ctx)
}

func TestCreateReconciliationJobChecksCapabilityAndIsRetrySafe(t *testing.T) {
	policies := &syncPolicyReaderStub{}
	guard := &syncCapabilityGuardStub{}
	store := &contractRunStore{}
	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/reconciliation/jobs", strings.NewReader(`{"policy_id":"policy-1"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "reconcile-policy-1")
		response := httptest.NewRecorder()
		createReconciliationJob(response, productionRequestContext(t, request), policies, store, guard)
		if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"policy_id":"policy-1"`) {
			t.Fatalf("attempt %d: status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
	if guard.calls != 2 || store.run.ID == "" || store.run.TriggerRef == "user|opaque" || !strings.HasPrefix(store.run.TriggerRef, "actor.") {
		t.Fatalf("guard=%d run=%+v", guard.calls, store.run)
	}
}

func TestCreatePrivacyRequestDerivesOpaqueStableIdentifiers(t *testing.T) {
	workflow := &privacyWorkflowStub{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/privacy/requests", strings.NewReader(`{"request_type":"deletion","subject_kind":"customer","subject_opaque_id":"opaque-customer-1"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "privacy-delete-1")
	response := httptest.NewRecorder()
	createPrivacyRequest(response, productionRequestContext(t, request), workflow)
	if response.Code != http.StatusAccepted || !strings.HasPrefix(workflow.spec.RequestID, "prq_") || !strings.HasPrefix(workflow.spec.JobID, "prj_") || strings.Contains(response.Body.String(), validTestScope(t).OrganizationID().String()) {
		t.Fatalf("status=%d spec=%+v body=%s", response.Code, workflow.spec, response.Body.String())
	}
}

func TestCreateUploadAcceptsGeneratedSDKJSONIntoQuarantineAdapter(t *testing.T) {
	receiver := &uploadReceiverStub{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/uploads", strings.NewReader(`{"filename":"items.csv","declared_media_type":"text/csv","content_base64":"YSxiCg=="}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "upload-items-1")
	response := httptest.NewRecorder()
	createUpload(response, productionRequestContext(t, request), receiver)
	if response.Code != http.StatusAccepted || receiver.content != "a,b\n" || receiver.metadata.OriginalFilename != "items.csv" || receiver.metadata.DeclaredMediaType != "text/csv" || receiver.metadata.DeclaredSizeBytes != 4 {
		t.Fatalf("status=%d metadata=%+v content=%q body=%s", response.Code, receiver.metadata, receiver.content, response.Body.String())
	}
}

func TestListCounterpartiesReturnsOnlyCanonicalCounterpartyRecords(t *testing.T) {
	repository := &counterpartyListerStub{}
	request := productionRequestContext(t, httptest.NewRequest(http.MethodGet, "/api/v1/counterparties", nil))
	response := httptest.NewRecorder()
	listCounterparties(response, request, repository)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"role":"supplier"`) || !strings.Contains(response.Body.String(), `"code":"supplier-1"`) || !repository.scope.Valid() {
		t.Fatalf("status=%d scope=%+v body=%s", response.Code, repository.scope, response.Body.String())
	}
}
