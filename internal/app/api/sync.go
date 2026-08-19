package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/reconciliation"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

const SyncStatusPath = "/api/v1/sync/status"
const SyncPoliciesPath = "/api/v1/sync/policies"
const SyncDriftsPath = "/api/v1/sync/drifts"

type syncPolicyReader interface {
	ListPolicies(context.Context, tenancy.Scope, int) ([]syncengine.Policy, error)
}

type syncPolicyRepository interface {
	syncPolicyReader
	CreatePolicy(context.Context, tenancy.Scope, syncengine.PolicyCreate) (syncengine.Policy, error)
	Policy(context.Context, tenancy.Scope, string) (syncengine.Policy, error)
	UpdatePolicy(context.Context, tenancy.Scope, syncengine.PolicyUpdate) (syncengine.Policy, error)
}

type createSyncPolicyInput struct {
	ID                 string                   `json:"id"`
	ConnectorAccountID string                   `json:"connector_account_id"`
	EntityType         string                   `json:"entity_type"`
	Direction          syncengine.Direction     `json:"direction"`
	SourceOfTruth      syncengine.SourceOfTruth `json:"source_of_truth"`
}
type updateSyncPolicyInput struct {
	Direction       syncengine.Direction     `json:"direction"`
	SourceOfTruth   syncengine.SourceOfTruth `json:"source_of_truth"`
	Enabled         bool                     `json:"enabled"`
	ExpectedVersion int64                    `json:"expected_version"`
}
type resolveSyncDriftInput struct {
	Action          reconciliation.ActionKind `json:"action"`
	ExpectedVersion int64                     `json:"expected_version"`
}

type reconciliationReader interface {
	ListRuns(context.Context, tenancy.Scope, int) ([]reconciliation.Run, error)
	ListRecentDrifts(context.Context, tenancy.Scope, int) ([]reconciliation.Drift, error)
}

type reconciliationWriter interface {
	CreateRun(context.Context, tenancy.Scope, reconciliation.Run) (reconciliation.Run, error)
}
type reconciliationRepository interface {
	reconciliationReader
	reconciliationWriter
	Drift(context.Context, tenancy.Scope, string) (reconciliation.Drift, error)
	UpdateDrift(context.Context, tenancy.Scope, reconciliation.Drift, int64) (reconciliation.Drift, error)
	RecordAction(context.Context, tenancy.Scope, reconciliation.ActionRecord) error
}
type connectorManualSync struct {
	policies syncPolicyReader
	runs     reconciliationWriter
	guard    syncPolicyCapabilityGuard
	previews interface {
		HasCurrentBootstrapPreview(context.Context, tenancy.Scope, string, time.Time) (bool, error)
	}
}

func (s connectorManualSync) Start(ctx context.Context, scope tenancy.Scope, accountID, actor string, at time.Time) (int, error) {
	items, err := s.policies.ListPolicies(ctx, scope, 100)
	if err != nil {
		return 0, err
	}
	candidates := make([]syncengine.Policy, 0)
	requiresPreview := false
	for _, policy := range items {
		accountMatches := sameSyncIdentity(policy.ConnectorAccountID, accountID)
		if !accountMatches || !policy.Enabled {
			continue
		}
		authorizationErr := authorizeSyncPolicy(ctx, s.guard, scope, policy.ConnectorAccountID, policy.EntityType, policy.Direction)
		if authorizationErr != nil {
			continue
		}
		candidates = append(candidates, policy)
		requiresPreview = requiresPreview || policy.Direction.AllowsOutbound()
	}
	if requiresPreview && s.previews != nil {
		hasPreview, previewErr := s.previews.HasCurrentBootstrapPreview(ctx, scope, accountID, at.UTC())
		if previewErr != nil || !hasPreview {
			return 0, syncengine.ErrPreviewUnavailable
		}
	}
	count := 0
	for _, policy := range candidates {
		id := "sync." + newApprovalID()
		_, err = s.runs.CreateRun(ctx, scope, reconciliation.Run{ID: id, PolicyID: policy.ID, Mode: reconciliation.ModeOnDemand, TriggerRef: actor, Status: reconciliation.RunRunning, Version: 1, StartedAt: at.UTC(), UpdatedAt: at.UTC()})
		if err != nil {
			return count, err
		}
		count++
	}
	if count == 0 {
		return 0, reconciliation.ErrNotFound
	}
	return count, nil
}

type syncPolicyView struct {
	ID                 string                   `json:"id"`
	ConnectorAccountID string                   `json:"connector_account_id"`
	EntityType         string                   `json:"entity_type"`
	Direction          syncengine.Direction     `json:"direction"`
	SourceOfTruth      syncengine.SourceOfTruth `json:"source_of_truth"`
	Enabled            bool                     `json:"enabled"`
	Version            int64                    `json:"version"`
	UpdatedAt          time.Time                `json:"updated_at"`
}
type syncRunView struct {
	ID           string                   `json:"id"`
	PolicyID     string                   `json:"policy_id"`
	Mode         reconciliation.Mode      `json:"mode"`
	Status       reconciliation.RunStatus `json:"status"`
	ScannedCount int64                    `json:"scanned_count"`
	DriftCount   int64                    `json:"drift_count"`
	StartedAt    time.Time                `json:"started_at"`
	UpdatedAt    time.Time                `json:"updated_at"`
}
type syncDriftView struct {
	ID                string                     `json:"id"`
	RunID             string                     `json:"run_id"`
	PolicyID          string                     `json:"policy_id"`
	Kind              reconciliation.DriftKind   `json:"kind"`
	LocalEntityID     string                     `json:"local_entity_id"`
	RemoteID          string                     `json:"remote_id"`
	LocalStatus       string                     `json:"local_status"`
	RemoteStatus      string                     `json:"remote_status"`
	Status            reconciliation.DriftStatus `json:"status"`
	RecommendedAction reconciliation.ActionKind  `json:"recommended_action"`
	Version           int64                      `json:"version"`
	DetectedAt        time.Time                  `json:"detected_at"`
}

func newSyncRoutes(policies syncPolicyRepository, reconciliations reconciliationRepository, guards ...syncPolicyCapabilityGuard) []ProtectedRoute {
	var guard syncPolicyCapabilityGuard
	if len(guards) > 0 {
		guard = guards[0]
	}
	return []ProtectedRoute{{Method: http.MethodPost, Path: SyncPoliciesPath, Permission: "sync.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := ScopeFromContext(r.Context())
		if !ok || policies == nil || strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		var input createSyncPolicyInput
		if err := decodeStrictJSON(r, &input); err != nil || input.ID != r.Header.Get("Idempotency-Key") {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		authorizationErr := authorizeSyncPolicy(r.Context(), guard, scope, input.ConnectorAccountID, input.EntityType, input.Direction)
		if authorizationErr != nil {
			writeProblem(w, http.StatusUnprocessableEntity, "Connector account capability required")
			return
		}
		policy, err := policies.CreatePolicy(r.Context(), scope, syncengine.PolicyCreate{ID: input.ID, ConnectorAccountID: input.ConnectorAccountID, EntityType: input.EntityType, Direction: input.Direction, SourceOfTruth: input.SourceOfTruth, Enabled: true})
		if errors.Is(err, syncengine.ErrInvalidRecord) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		if err != nil {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		writeJSON(w, http.StatusCreated, policyView(policy))
	})}, {Method: http.MethodPatch, Path: SyncPoliciesPath + "/", PathPrefix: true, Permission: "sync.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		syncPolicyAction(w, r, policies, reconciliations, guard)
	})}, {Method: http.MethodPost, Path: SyncPoliciesPath + "/", PathPrefix: true, Permission: "sync.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		syncPolicyAction(w, r, policies, reconciliations, guard)
	})}, {Method: http.MethodPost, Path: SyncDriftsPath + "/", PathPrefix: true, Permission: "sync.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resolveSyncDrift(w, r, reconciliations)
	})}, {Method: http.MethodGet, Path: SyncStatusPath, Permission: "sync.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := ScopeFromContext(r.Context())
		if !ok || policies == nil || reconciliations == nil {
			writeProblem(w, http.StatusForbidden, "Forbidden")
			return
		}
		policyItems, err := policies.ListPolicies(r.Context(), scope, 100)
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		runs, err := reconciliations.ListRuns(r.Context(), scope, 50)
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		drifts, err := reconciliations.ListRecentDrifts(r.Context(), scope, 100)
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		enabled, active, open := 0, 0, 0
		for _, policy := range policyItems {
			if policy.Enabled {
				enabled++
			}
		}
		for _, run := range runs {
			if run.Status == reconciliation.RunRunning {
				active++
			}
		}
		for _, drift := range drifts {
			if drift.Status == reconciliation.DriftOpen {
				open++
			}
		}
		policyViews := make([]syncPolicyView, 0, len(policyItems))
		for _, v := range policyItems {
			policyViews = append(policyViews, policyView(v))
		}
		runViews := make([]syncRunView, 0, len(runs))
		for _, v := range runs {
			runViews = append(runViews, syncRunView{v.ID, v.PolicyID, v.Mode, v.Status, v.ScannedCount, v.DriftCount, v.StartedAt, v.UpdatedAt})
		}
		driftViews := make([]syncDriftView, 0, len(drifts))
		for _, v := range drifts {
			driftViews = append(driftViews, driftView(v))
		}
		writeJSON(w, http.StatusOK, map[string]any{"summary": map[string]int{"enabled_policies": enabled, "active_runs": active, "open_drifts": open}, "policies": policyViews, "runs": runViews, "drifts": driftViews})
	})}}
}

func policyView(v syncengine.Policy) syncPolicyView {
	return syncPolicyView{v.ID, v.ConnectorAccountID, v.EntityType, v.Direction, v.SourceOfTruth, v.Enabled, v.Version, v.UpdatedAt}
}

func driftView(v reconciliation.Drift) syncDriftView {
	return syncDriftView{v.ID, v.RunID, v.PolicyID, v.Kind, v.LocalEntityID, v.RemoteID, v.LocalStatus, v.RemoteStatus, v.Status, v.RecommendedAction, v.Version, v.DetectedAt}
}

func syncPolicyAction(w http.ResponseWriter, r *http.Request, policies syncPolicyRepository, runs reconciliationWriter, guard syncPolicyCapabilityGuard) {
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, SyncPoliciesPath+"/"), "/"), "/")
	if !ok || len(parts) == 0 || parts[0] == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "run" {
		if !principalOK {
			writeProblem(w, http.StatusForbidden, "Forbidden")
			return
		}
		policy, err := policies.Policy(r.Context(), scope, parts[0])
		if err != nil || !policy.Enabled {
			writeProblem(w, http.StatusUnprocessableEntity, "Enabled policy required")
			return
		}
		authorizationErr := authorizeSyncPolicy(r.Context(), guard, scope, policy.ConnectorAccountID, policy.EntityType, policy.Direction)
		if authorizationErr != nil {
			writeProblem(w, http.StatusUnprocessableEntity, "Connector account capability required")
			return
		}
		now := time.Now().UTC()
		run, err := runs.CreateRun(r.Context(), scope, reconciliation.Run{ID: "sync." + newApprovalID(), PolicyID: policy.ID, Mode: reconciliation.ModeOnDemand, TriggerRef: principal.Subject, Status: reconciliation.RunRunning, Version: 1, StartedAt: now, UpdatedAt: now})
		if err != nil {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		writeJSON(w, http.StatusAccepted, syncRunView{run.ID, run.PolicyID, run.Mode, run.Status, run.ScannedCount, run.DriftCount, run.StartedAt, run.UpdatedAt})
		return
	}
	if r.Method != http.MethodPatch || len(parts) != 1 {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	var input updateSyncPolicyInput
	if decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if input.Enabled && guard != nil {
		current, err := policies.Policy(r.Context(), scope, parts[0])
		if err != nil {
			writeProblem(w, http.StatusUnprocessableEntity, "Connector account capability required")
			return
		}
		authorizationErr := authorizeSyncPolicy(r.Context(), guard, scope, current.ConnectorAccountID, current.EntityType, input.Direction)
		if authorizationErr != nil {
			writeProblem(w, http.StatusUnprocessableEntity, "Connector account capability required")
			return
		}
	}
	updated, err := policies.UpdatePolicy(r.Context(), scope, syncengine.PolicyUpdate{ID: parts[0], Direction: input.Direction, SourceOfTruth: input.SourceOfTruth, Enabled: input.Enabled, ExpectedVersion: input.ExpectedVersion})
	if errors.Is(err, syncengine.ErrPolicyConflict) {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	writeJSON(w, http.StatusOK, policyView(updated))
}

func authorizeSyncPolicy(ctx context.Context, guard syncPolicyCapabilityGuard, scope tenancy.Scope, accountID, entityType string, direction syncengine.Direction) error {
	if guard == nil {
		return nil
	}
	return guard.AuthorizePolicy(ctx, scope, accountID, entityType, direction)
}

func sameSyncIdentity(left, right string) bool { return strings.Compare(left, right) == 0 }

func resolveSyncDrift(w http.ResponseWriter, r *http.Request, repository reconciliationRepository) {
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	scope, ok := ScopeFromContext(r.Context())
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, SyncDriftsPath+"/"), "/"), "/")
	if !ok || len(parts) != 2 || parts[0] == "" || parts[1] != "actions" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	var input resolveSyncDriftInput
	if decodeStrictJSON(r, &input) != nil || input.Action != reconciliation.ActionIgnore {
		writeProblem(w, http.StatusUnprocessableEntity, "Only safe ignore action is available")
		return
	}
	drift, err := repository.Drift(r.Context(), scope, parts[0])
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if drift.Status != reconciliation.DriftOpen || drift.Version != input.ExpectedVersion {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	now := time.Now().UTC()
	drift.Status, drift.ResolvedAt = reconciliation.DriftIgnored, &now
	updated, err := repository.UpdateDrift(r.Context(), scope, drift, input.ExpectedVersion)
	if err != nil {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if err = repository.RecordAction(r.Context(), scope, reconciliation.ActionRecord{ID: "action." + newApprovalID(), DriftID: drift.ID, Action: reconciliation.ActionIgnore, IdempotencyKey: r.Header.Get("Idempotency-Key"), Result: reconciliation.ActionSucceeded, CreatedAt: now}); err != nil {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	writeJSON(w, http.StatusOK, driftView(updated))
}
