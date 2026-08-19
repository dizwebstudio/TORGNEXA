package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

type bootstrapAccountStub struct{ account sdk.Account }

func (s bootstrapAccountStub) AccountByID(context.Context, string, string, string) (sdk.Account, error) {
	return s.account, nil
}

type bootstrapStoreStub struct {
	policies   []syncengine.Policy
	preview    syncengine.BootstrapPreview
	hasPreview bool
	schedule   syncengine.AccountSchedule
}

func (s *bootstrapStoreStub) ListPolicies(context.Context, tenancy.Scope, int) ([]syncengine.Policy, error) {
	return s.policies, nil
}
func (s *bootstrapStoreStub) CreateBootstrapPreview(_ context.Context, _ tenancy.Scope, v syncengine.BootstrapPreview) (syncengine.BootstrapPreview, error) {
	s.preview = v
	return v, nil
}
func (s *bootstrapStoreStub) HasCurrentBootstrapPreview(context.Context, tenancy.Scope, string, time.Time) (bool, error) {
	return s.hasPreview, nil
}
func (s *bootstrapStoreStub) CreateInitialJob(_ context.Context, scope tenancy.Scope, previewID, jobID string, at time.Time) (syncengine.SyncJob, error) {
	return syncengine.SyncJob{ID: jobID, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), AccountID: s.preview.AccountID, Kind: syncengine.SyncJobInitialImport, Mode: syncengine.ScheduleIncremental, Status: syncengine.SyncJobPending, PreviewID: previewID, MaxAttempts: 5, AvailableAt: at, CreatedAt: at, UpdatedAt: at}, nil
}
func (s *bootstrapStoreStub) PutAccountSchedule(_ context.Context, _ tenancy.Scope, v syncengine.AccountSchedule, _ int64) (syncengine.AccountSchedule, error) {
	s.schedule = v
	return v, nil
}
func (s *bootstrapStoreStub) ListBootstrapPreviews(context.Context, tenancy.Scope, int) ([]syncengine.BootstrapPreview, error) {
	return nil, nil
}
func (s *bootstrapStoreStub) ListAccountSchedules(context.Context, tenancy.Scope, int) ([]syncengine.AccountSchedule, error) {
	return nil, nil
}
func (s *bootstrapStoreStub) ListSyncJobs(context.Context, tenancy.Scope, int) ([]syncengine.SyncJob, error) {
	return nil, nil
}

type bootstrapAuditStub struct{ actions []string }

func (s *bootstrapAuditStub) Capture(_ context.Context, _ tenancy.Scope, entry audit.Entry) (audit.Record, error) {
	s.actions = append(s.actions, entry.Action)
	return audit.Record{}, nil
}

func bootstrapRequestContext(t *testing.T, request *http.Request) *http.Request {
	t.Helper()
	ctx := context.WithValue(request.Context(), requestScopeKey{}, validTestScope(t))
	ctx = context.WithValue(ctx, requestIdentityKey{}, Principal{Issuer: "https://id.example.test", Subject: "admin|opaque"})
	return request.WithContext(ctx)
}

func TestBootstrapPreviewIsMetadataOnlyAndAudited(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	account := sdk.Account{ID: "cabinet-1", Version: 7, Status: sdk.AccountActive, Health: sdk.Health{Status: sdk.HealthHealthy}}
	store := &bootstrapStoreStub{policies: []syncengine.Policy{{ID: "policy-1", ConnectorAccountID: account.ID, EntityType: "orders", Direction: syncengine.DirectionBidirectional, Enabled: true}}}
	auditor := &bootstrapAuditStub{}
	api := connectorBootstrapAPI{accounts: bootstrapAccountStub{account}, store: store, audit: auditor, now: func() time.Time { return now }}
	request := httptest.NewRequest(http.MethodPost, ConnectorBootstrapPreviewPath, strings.NewReader(`{"account_id":"cabinet-1","expected_version":7}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "preview-1")
	response := httptest.NewRecorder()
	api.preview(response, bootstrapRequestContext(t, request))
	if response.Code != http.StatusCreated || store.preview.PolicyCount != 1 || store.preview.ReadCount != 1 || store.preview.WriteCount != 1 || len(auditor.actions) != 1 || !strings.Contains(response.Body.String(), `"expires_at"`) {
		t.Fatalf("status=%d preview=%+v audit=%v body=%s", response.Code, store.preview, auditor.actions, response.Body.String())
	}
}

func TestEnabledScheduleRequiresCurrentPreview(t *testing.T) {
	account := sdk.Account{ID: "cabinet-1", Version: 7, Status: sdk.AccountActive, Health: sdk.Health{Status: sdk.HealthHealthy}}
	store, auditor := &bootstrapStoreStub{}, &bootstrapAuditStub{}
	api := connectorBootstrapAPI{accounts: bootstrapAccountStub{account}, store: store, audit: auditor, now: func() time.Time { return time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC) }}
	request := httptest.NewRequest(http.MethodPut, ConnectorSchedulePath, strings.NewReader(`{"account_id":"cabinet-1","account_version":7,"mode":"incremental","interval_minutes":60,"enabled":true,"expected_version":0}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "schedule-1")
	response := httptest.NewRecorder()
	api.putSchedule(response, bootstrapRequestContext(t, request))
	if response.Code != http.StatusUnprocessableEntity || store.schedule.AccountID != "" {
		t.Fatalf("status=%d schedule=%+v body=%s", response.Code, store.schedule, response.Body.String())
	}
}
