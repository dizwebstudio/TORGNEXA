package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/reconciliation"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

type syncPolicyReaderStub struct {
	scope   tenancy.Scope
	created bool
}

func (s *syncPolicyReaderStub) ListPolicies(_ context.Context, scope tenancy.Scope, _ int) ([]syncengine.Policy, error) {
	s.scope = scope
	return []syncengine.Policy{{ID: "policy-1", ConnectorAccountID: "cabinet-1", EntityType: "order", Direction: syncengine.DirectionInbound, SourceOfTruth: syncengine.SourceRemote, Enabled: true, UpdatedAt: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)}}, nil
}
func (s *syncPolicyReaderStub) CreatePolicy(_ context.Context, scope tenancy.Scope, command syncengine.PolicyCreate) (syncengine.Policy, error) {
	s.scope = scope
	s.created = true
	return syncengine.Policy{ID: command.ID, ConnectorAccountID: command.ConnectorAccountID, EntityType: command.EntityType, Direction: command.Direction, SourceOfTruth: command.SourceOfTruth, Enabled: command.Enabled, UpdatedAt: time.Now().UTC()}, nil
}

type syncCapabilityGuardStub struct {
	err   error
	calls int
}

func (s *syncCapabilityGuardStub) AuthorizePolicy(_ context.Context, _ tenancy.Scope, _, _ string, _ syncengine.Direction) error {
	s.calls++
	return s.err
}
func (s *syncPolicyReaderStub) Policy(_ context.Context, scope tenancy.Scope, id string) (syncengine.Policy, error) {
	s.scope = scope
	return syncengine.Policy{ID: id, ConnectorAccountID: "cabinet-1", EntityType: "orders", Direction: syncengine.DirectionInbound, SourceOfTruth: syncengine.SourceRemote, Enabled: true, Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, nil
}
func (s *syncPolicyReaderStub) UpdatePolicy(_ context.Context, scope tenancy.Scope, command syncengine.PolicyUpdate) (syncengine.Policy, error) {
	s.scope = scope
	return syncengine.Policy{ID: command.ID, ConnectorAccountID: "cabinet-1", EntityType: "orders", Direction: command.Direction, SourceOfTruth: command.SourceOfTruth, Enabled: command.Enabled, Version: command.ExpectedVersion + 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, nil
}

type reconciliationReaderStub struct{ scope tenancy.Scope }

func (s *reconciliationReaderStub) ListRuns(_ context.Context, scope tenancy.Scope, _ int) ([]reconciliation.Run, error) {
	s.scope = scope
	return []reconciliation.Run{{ID: "run-1", PolicyID: "policy-1", Mode: reconciliation.ModeOnDemand, Status: reconciliation.RunRunning}}, nil
}
func (s *reconciliationReaderStub) ListRecentDrifts(_ context.Context, scope tenancy.Scope, _ int) ([]reconciliation.Drift, error) {
	s.scope = scope
	return []reconciliation.Drift{{ID: "drift-1", RunID: "run-1", PolicyID: "policy-1", Kind: reconciliation.DriftStatusMismatch, Status: reconciliation.DriftOpen, RecommendedAction: reconciliation.ActionNotify}}, nil
}
func (s *reconciliationReaderStub) CreateRun(_ context.Context, scope tenancy.Scope, run reconciliation.Run) (reconciliation.Run, error) {
	s.scope = scope
	return run, nil
}
func (s *reconciliationReaderStub) Run(_ context.Context, scope tenancy.Scope, id string) (reconciliation.Run, error) {
	s.scope = scope
	return reconciliation.Run{ID: id, PolicyID: "policy-1", Mode: reconciliation.ModeOnDemand, Status: reconciliation.RunRunning, Version: 1, StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, nil
}
func (s *reconciliationReaderStub) Drift(_ context.Context, scope tenancy.Scope, id string) (reconciliation.Drift, error) {
	s.scope = scope
	return reconciliation.Drift{ID: id, RunID: "run-1", PolicyID: "policy-1", Kind: reconciliation.DriftStatusMismatch, Status: reconciliation.DriftOpen, RecommendedAction: reconciliation.ActionIgnore, Version: 1, DetectedAt: time.Now().UTC()}, nil
}
func (s *reconciliationReaderStub) UpdateDrift(_ context.Context, scope tenancy.Scope, drift reconciliation.Drift, _ int64) (reconciliation.Drift, error) {
	s.scope = scope
	drift.Version++
	return drift, nil
}
func (s *reconciliationReaderStub) RecordAction(_ context.Context, scope tenancy.Scope, _ reconciliation.ActionRecord) error {
	s.scope = scope
	return nil
}

func TestSyncStatusUsesAuthorizedScopeAndSummarizesState(t *testing.T) {
	scope := validTestScope(t)
	policies := &syncPolicyReaderStub{}
	reconciliations := &reconciliationReaderStub{}
	var route ProtectedRoute
	for _, candidate := range newSyncRoutes(policies, reconciliations) {
		if candidate.Method == http.MethodGet && candidate.Path == SyncStatusPath {
			route = candidate
			break
		}
	}
	request := httptest.NewRequest(http.MethodGet, SyncStatusPath, nil)
	request = request.WithContext(context.WithValue(request.Context(), requestScopeKey{}, scope))
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || policies.scope != scope || reconciliations.scope != scope || !strings.Contains(response.Body.String(), `"open_drifts":1`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSyncPolicyCanBeUpdated(t *testing.T) {
	scope := validTestScope(t)
	policies, reconciliations := &syncPolicyReaderStub{}, &reconciliationReaderStub{}
	request := httptest.NewRequest(http.MethodPatch, SyncPoliciesPath+"/policy-1", strings.NewReader(`{"direction":"bidirectional","source_of_truth":"local","enabled":false,"expected_version":1}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "update-policy-1")
	request = request.WithContext(context.WithValue(request.Context(), requestScopeKey{}, scope))
	response := httptest.NewRecorder()
	newSyncRoutes(policies, reconciliations)[1].Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"enabled":false`) || !strings.Contains(response.Body.String(), `"version":2`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSyncPolicyCreationFailsClosedWithoutAccountCapability(t *testing.T) {
	scope := validTestScope(t)
	policies, reconciliations := &syncPolicyReaderStub{}, &reconciliationReaderStub{}
	guard := &syncCapabilityGuardStub{err: errSyncCapabilityDenied}
	request := httptest.NewRequest(http.MethodPost, SyncPoliciesPath, strings.NewReader(`{"id":"policy-denied","connector_account_id":"cabinet-1","entity_type":"orders","direction":"inbound","source_of_truth":"remote"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "policy-denied")
	request = request.WithContext(context.WithValue(request.Context(), requestScopeKey{}, scope))
	response := httptest.NewRecorder()
	newSyncRoutes(policies, reconciliations, guard)[0].Handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || guard.calls != 1 || policies.created {
		t.Fatalf("status=%d guard_calls=%d created=%v body=%s", response.Code, guard.calls, policies.created, response.Body.String())
	}
}

func TestOpenDriftCanBeIgnored(t *testing.T) {
	scope := validTestScope(t)
	policies, reconciliations := &syncPolicyReaderStub{}, &reconciliationReaderStub{}
	request := httptest.NewRequest(http.MethodPost, SyncDriftsPath+"/drift-1/actions", strings.NewReader(`{"action":"ignore","expected_version":1}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "ignore-drift-1")
	request = request.WithContext(context.WithValue(request.Context(), requestScopeKey{}, scope))
	response := httptest.NewRecorder()
	newSyncRoutes(policies, reconciliations)[3].Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"ignored"`) || !strings.Contains(response.Body.String(), `"version":2`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
