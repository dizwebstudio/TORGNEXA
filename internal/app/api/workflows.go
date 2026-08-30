package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/core/workflow"
)

const (
	WorkflowsPath    = "/api/v1/workflows"
	WorkflowRunsPath = "/api/v1/workflow-runs"
)

type workflowStore interface {
	Create(context.Context, workflow.Scope, string, workflow.Definition) (workflow.Workflow, error)
	List(context.Context, workflow.Scope, int) ([]workflow.Workflow, error)
	Get(context.Context, workflow.Scope, string) (workflow.Workflow, error)
	Version(context.Context, workflow.Scope, string, int64) (workflow.WorkflowVersion, error)
	Publish(context.Context, workflow.Scope, string, int64, workflow.Definition) (workflow.Workflow, error)
	ChangeStatus(context.Context, workflow.Scope, string, int64, workflow.DefinitionStatus) (workflow.Workflow, error)
	CreateRun(context.Context, workflow.Scope, workflow.RunRequest) (workflow.Run, error)
	GetRun(context.Context, workflow.Scope, string) (workflow.Run, error)
	ListRuns(context.Context, workflow.Scope, int) ([]workflow.Run, error)
	ListSteps(context.Context, workflow.Scope, string, int) ([]workflow.StepRun, error)
	ListEvidence(context.Context, workflow.Scope, string, int) ([]workflow.ExecutionEvidence, error)
	CancelRun(context.Context, workflow.Scope, string, int64) (workflow.Run, error)
}

type workflowView struct {
	ID             string                    `json:"id"`
	OrganizationID string                    `json:"organization_id"`
	WorkspaceID    string                    `json:"workspace_id"`
	Name           string                    `json:"name"`
	Description    string                    `json:"description,omitempty"`
	Status         workflow.DefinitionStatus `json:"status"`
	CurrentVersion int64                     `json:"current_version"`
	Version        int64                     `json:"version"`
	CreatedAt      string                    `json:"created_at"`
	UpdatedAt      string                    `json:"updated_at"`
}

type workflowRunView struct {
	ID              string               `json:"id"`
	WorkflowID      string               `json:"workflow_id"`
	WorkflowVersion int64                `json:"workflow_version"`
	TriggerKind     workflow.TriggerKind `json:"trigger_kind"`
	TriggerRef      string               `json:"trigger_ref,omitempty"`
	Status          workflow.RunStatus   `json:"status"`
	AttemptCount    int                  `json:"attempt_count"`
	AvailableAt     string               `json:"available_at"`
	StartedAt       *string              `json:"started_at,omitempty"`
	CompletedAt     *string              `json:"completed_at,omitempty"`
	LastErrorCode   string               `json:"last_error_code,omitempty"`
	Version         int64                `json:"version"`
}

type workflowStepView struct {
	ID           string              `json:"id"`
	RunID        string              `json:"run_id"`
	NodeID       string              `json:"node_id"`
	Status       workflow.StepStatus `json:"status"`
	AttemptCount int                 `json:"attempt_count"`
	OutputDigest string              `json:"output_digest,omitempty"`
	ErrorCode    string              `json:"error_code,omitempty"`
	StartedAt    *string             `json:"started_at,omitempty"`
	CompletedAt  *string             `json:"completed_at,omitempty"`
	Version      int64               `json:"version"`
}

type workflowEvidenceView struct {
	ID           string              `json:"id"`
	RunID        string              `json:"run_id"`
	NodeID       string              `json:"node_id"`
	Attempt      int                 `json:"attempt"`
	Outcome      workflow.StepStatus `json:"outcome"`
	OutputDigest string              `json:"output_digest,omitempty"`
	ErrorCode    string              `json:"error_code,omitempty"`
	ObservedAt   string              `json:"observed_at"`
}

func newWorkflowRoutes(store workflowStore) []ProtectedRoute {
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: WorkflowsPath, Permission: "workflows.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := workflowScope(r)
			if !ok || store == nil {
				writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
				return
			}
			limit, good := boundedLimit(r, 50, 200)
			if !good {
				writeProblem(w, http.StatusBadRequest, "Bad Request")
				return
			}
			items, err := store.List(r.Context(), scope, limit)
			if err != nil {
				writeWorkflowError(w, err)
				return
			}
			views := make([]workflowView, 0, len(items))
			for _, item := range items {
				views = append(views, toWorkflowView(item))
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": views})
		})},
		{Method: http.MethodPost, Path: WorkflowsPath, Permission: "workflows.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := workflowScope(r)
			key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			if !ok || store == nil || !validIdempotencyKey(key) {
				writeProblem(w, http.StatusBadRequest, "Bad Request")
				return
			}
			var input struct {
				ID         string              `json:"id"`
				Definition workflow.Definition `json:"definition"`
			}
			if decodeStrictJSON(r, &input) != nil || input.Definition.Validate() != nil {
				writeProblem(w, http.StatusBadRequest, "Bad Request")
				return
			}
			if input.ID == "" {
				input.ID = stableID("wf_", 32, tenancyScope(scope), key)
			}
			item, err := store.Create(r.Context(), scope, input.ID, input.Definition)
			if err != nil {
				writeWorkflowError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, toWorkflowView(item))
		})},
		{Method: http.MethodPost, Path: "/api/v1/workflow-commands/validate", Permission: "workflows.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var definition workflow.Definition
			if decodeStrictJSON(r, &definition) != nil {
				writeProblem(w, http.StatusBadRequest, "Bad Request")
				return
			}
			plan, err := workflow.Compile(definition)
			if err != nil {
				writeWorkflowError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"valid": true, "plan_digest": plan.Digest, "node_ids": plan.NodeIDs, "limits": map[string]int{"max_nodes": workflow.MaxNodes, "max_edges": workflow.MaxEdges}})
		})},
		{Method: http.MethodPost, Path: "/api/v1/workflow-commands/publish", Permission: "workflows.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := workflowScope(r)
			key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			if !ok || store == nil || !validIdempotencyKey(key) {
				writeProblem(w, http.StatusBadRequest, "Bad Request")
				return
			}
			var input struct {
				WorkflowID      string              `json:"workflow_id"`
				ExpectedVersion int64               `json:"expected_version"`
				Definition      workflow.Definition `json:"definition"`
			}
			if decodeStrictJSON(r, &input) != nil || input.WorkflowID == "" || input.ExpectedVersion < 1 {
				writeProblem(w, http.StatusBadRequest, "Bad Request")
				return
			}
			item, err := store.Publish(r.Context(), scope, input.WorkflowID, input.ExpectedVersion, input.Definition)
			if err != nil {
				writeWorkflowError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, toWorkflowView(item))
		})},
		{Method: http.MethodPost, Path: "/api/v1/workflow-commands/pause", Permission: "workflows.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { workflowStatusAction(w, r, store, workflow.StatusPaused) })},
		{Method: http.MethodPost, Path: "/api/v1/workflow-commands/archive", Permission: "workflows.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			workflowStatusAction(w, r, store, workflow.StatusArchived)
		})},
		{Method: http.MethodPost, Path: "/api/v1/workflow-commands/run", Permission: "workflows.run", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := workflowScope(r)
			key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			if !ok || store == nil || !validIdempotencyKey(key) {
				writeProblem(w, http.StatusBadRequest, "Bad Request")
				return
			}
			var input struct {
				WorkflowID      string `json:"workflow_id"`
				WorkflowVersion int64  `json:"workflow_version"`
				TriggerRef      string `json:"trigger_ref"`
				InputDigest     string `json:"input_digest"`
			}
			if decodeStrictJSON(r, &input) != nil || input.WorkflowID == "" || input.WorkflowVersion < 1 {
				writeProblem(w, http.StatusBadRequest, "Bad Request")
				return
			}
			if input.InputDigest == "" {
				digest := sha256.Sum256([]byte("workflow.manual.empty-input.v1"))
				input.InputDigest = hex.EncodeToString(digest[:])
			}
			version, versionErr := store.Version(r.Context(), scope, input.WorkflowID, input.WorkflowVersion)
			if versionErr != nil {
				writeWorkflowError(w, versionErr)
				return
			}
			if version.PublishedAt == nil {
				writeProblem(w, http.StatusConflict, "Conflict")
				return
			}
			run, err := store.CreateRun(r.Context(), scope, workflow.RunRequest{ID: stableID("run_", 32, tenancyScope(scope), key), WorkflowID: input.WorkflowID, WorkflowVersion: input.WorkflowVersion, TriggerKind: workflow.TriggerEvent, TriggerRef: input.TriggerRef, IdempotencyKey: key, InputDigest: input.InputDigest})
			if err != nil {
				writeWorkflowError(w, err)
				return
			}
			writeJSON(w, http.StatusAccepted, toWorkflowRunView(run))
		})},
		{Method: http.MethodGet, Path: WorkflowRunsPath, Permission: "workflows.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { listWorkflowRuns(w, r, store) })},
		{Method: http.MethodPost, Path: "/api/v1/workflow-run-commands/retry", Permission: "workflows.run", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := workflowScope(r)
			key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			if !ok || store == nil || !validIdempotencyKey(key) {
				writeProblem(w, 400, "Bad Request")
				return
			}
			var input struct {
				RunID string `json:"run_id"`
			}
			if decodeStrictJSON(r, &input) != nil || input.RunID == "" {
				writeProblem(w, 400, "Bad Request")
				return
			}
			original, err := store.GetRun(r.Context(), scope, input.RunID)
			if err != nil {
				writeWorkflowError(w, err)
				return
			}
			if original.Status != workflow.RunFailed && original.Status != workflow.RunCancelled {
				writeProblem(w, 409, "Conflict")
				return
			}
			retry, err := store.CreateRun(r.Context(), scope, workflow.RunRequest{ID: stableID("run_", 32, tenancyScope(scope), key), WorkflowID: original.WorkflowID, WorkflowVersion: original.WorkflowVersion, TriggerKind: original.TriggerKind, TriggerRef: "replay:" + original.ID, IdempotencyKey: key, InputDigest: original.InputDigest})
			if err != nil {
				writeWorkflowError(w, err)
				return
			}
			writeJSON(w, http.StatusAccepted, toWorkflowRunView(retry))
		})},
		{Method: http.MethodPost, Path: "/api/v1/workflow-run-commands/cancel", Permission: "workflows.run", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := workflowScope(r)
			key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			if !ok || store == nil || !validIdempotencyKey(key) {
				writeProblem(w, 400, "Bad Request")
				return
			}
			var input struct {
				RunID           string `json:"run_id"`
				ExpectedVersion int64  `json:"expected_version"`
			}
			if decodeStrictJSON(r, &input) != nil || input.RunID == "" || input.ExpectedVersion < 1 {
				writeProblem(w, 400, "Bad Request")
				return
			}
			item, err := store.CancelRun(r.Context(), scope, input.RunID, input.ExpectedVersion)
			if err != nil {
				writeWorkflowError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, toWorkflowRunView(item))
		})},
		{Method: http.MethodGet, Path: WorkflowsPath + "/", PathPrefix: true, Permission: "workflows.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { workflowByPath(w, r, store) })},
		{Method: http.MethodGet, Path: WorkflowRunsPath + "/", PathPrefix: true, Permission: "workflows.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { workflowRunByPath(w, r, store) })},
	}
}

func workflowScope(r *http.Request) (workflow.Scope, bool) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok {
		return workflow.Scope{}, false
	}
	converted, err := workflow.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	return converted, err == nil
}
func tenancyScope(scope workflow.Scope) tenancy.Scope {
	converted, _ := tenancy.ParseScope(scope.OrganizationID(), scope.WorkspaceID())
	return converted
}
func toWorkflowView(v workflow.Workflow) workflowView {
	return workflowView{v.ID, v.OrganizationID, v.WorkspaceID, v.Name, v.Description, v.Status, v.CurrentVersion, v.Version, v.CreatedAt.Format(time.RFC3339Nano), v.UpdatedAt.Format(time.RFC3339Nano)}
}
func toWorkflowRunView(v workflow.Run) workflowRunView {
	var started, completed *string
	if v.StartedAt != nil {
		s := v.StartedAt.Format(time.RFC3339Nano)
		started = &s
	}
	if v.CompletedAt != nil {
		s := v.CompletedAt.Format(time.RFC3339Nano)
		completed = &s
	}
	return workflowRunView{v.ID, v.WorkflowID, v.WorkflowVersion, v.TriggerKind, v.TriggerRef, v.Status, v.AttemptCount, v.AvailableAt.Format(time.RFC3339Nano), started, completed, v.LastErrorCode, v.Version}
}
func listWorkflowRuns(w http.ResponseWriter, r *http.Request, store workflowStore) {
	scope, ok := workflowScope(r)
	if !ok || store == nil {
		writeProblem(w, 503, "Service Unavailable")
		return
	}
	limit, good := boundedLimit(r, 50, 200)
	if !good {
		writeProblem(w, 400, "Bad Request")
		return
	}
	items, err := store.ListRuns(r.Context(), scope, limit)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	views := make([]workflowRunView, 0, len(items))
	for _, item := range items {
		views = append(views, toWorkflowRunView(item))
	}
	writeJSON(w, 200, map[string]any{"items": views})
}
func workflowByPath(w http.ResponseWriter, r *http.Request, store workflowStore) {
	id := strings.TrimPrefix(r.URL.Path, WorkflowsPath+"/")
	if id == "" || strings.Contains(id, "/") {
		writeProblem(w, 400, "Bad Request")
		return
	}
	scope, ok := workflowScope(r)
	if !ok || store == nil {
		writeProblem(w, 503, "Service Unavailable")
		return
	}
	item, err := store.Get(r.Context(), scope, id)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, 200, toWorkflowView(item))
}
func workflowRunByPath(w http.ResponseWriter, r *http.Request, store workflowStore) {
	rest := strings.TrimPrefix(r.URL.Path, WorkflowRunsPath+"/")
	parts := strings.Split(rest, "/")
	if len(parts) == 2 && (parts[1] == "steps" || parts[1] == "evidence") {
		workflowRunSubresource(w, r, store, parts[0], parts[1])
		return
	}
	id := rest
	if id == "" || strings.Contains(id, "/") {
		writeProblem(w, 400, "Bad Request")
		return
	}
	scope, ok := workflowScope(r)
	if !ok || store == nil {
		writeProblem(w, 503, "Service Unavailable")
		return
	}
	item, err := store.GetRun(r.Context(), scope, id)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, 200, toWorkflowRunView(item))
}

func workflowRunSubresource(w http.ResponseWriter, r *http.Request, store workflowStore, runID, resource string) {
	if runID == "" || strings.Contains(runID, "/") {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	scope, ok := workflowScope(r)
	if !ok || store == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	if _, err := store.GetRun(r.Context(), scope, runID); err != nil {
		writeWorkflowError(w, err)
		return
	}
	limit, good := boundedLimit(r, 100, 200)
	if !good {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	switch resource {
	case "steps":
		items, err := store.ListSteps(r.Context(), scope, runID, limit)
		if err != nil {
			writeWorkflowError(w, err)
			return
		}
		views := make([]workflowStepView, 0, len(items))
		for _, item := range items {
			views = append(views, toWorkflowStepView(item))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": views})
	case "evidence":
		items, err := store.ListEvidence(r.Context(), scope, runID, limit)
		if err != nil {
			writeWorkflowError(w, err)
			return
		}
		views := make([]workflowEvidenceView, 0, len(items))
		for _, item := range items {
			views = append(views, toWorkflowEvidenceView(item))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": views})
	}
}

func toWorkflowStepView(v workflow.StepRun) workflowStepView {
	return workflowStepView{ID: v.ID, RunID: v.RunID, NodeID: v.NodeID, Status: v.Status, AttemptCount: v.AttemptCount, OutputDigest: v.OutputDigest, ErrorCode: v.ErrorCode, StartedAt: formatOptionalTime(v.StartedAt), CompletedAt: formatOptionalTime(v.CompletedAt), Version: v.Version}
}

func toWorkflowEvidenceView(v workflow.ExecutionEvidence) workflowEvidenceView {
	return workflowEvidenceView{ID: v.ID, RunID: v.RunID, NodeID: v.NodeID, Attempt: v.Attempt, Outcome: v.Outcome, OutputDigest: v.OutputDigest, ErrorCode: v.ErrorCode, ObservedAt: v.ObservedAt.Format(time.RFC3339Nano)}
}

func formatOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format(time.RFC3339Nano)
	return &formatted
}
func workflowStatusAction(w http.ResponseWriter, r *http.Request, store workflowStore, next workflow.DefinitionStatus) {
	scope, ok := workflowScope(r)
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !ok || store == nil || !validIdempotencyKey(key) {
		writeProblem(w, 400, "Bad Request")
		return
	}
	var input struct {
		WorkflowID      string `json:"workflow_id"`
		ExpectedVersion int64  `json:"expected_version"`
	}
	if decodeStrictJSON(r, &input) != nil || input.WorkflowID == "" || input.ExpectedVersion < 1 {
		writeProblem(w, 400, "Bad Request")
		return
	}
	item, err := store.ChangeStatus(r.Context(), scope, input.WorkflowID, input.ExpectedVersion, next)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, 200, toWorkflowView(item))
}
func writeWorkflowError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workflow.ErrConflict):
		writeProblem(w, 409, "Conflict")
	case errors.Is(err, workflow.ErrQuotaExceeded):
		writeProblem(w, http.StatusTooManyRequests, "Too Many Requests")
	case errors.Is(err, workflow.ErrNotFound):
		writeProblem(w, 404, "Not Found")
	case errors.Is(err, workflow.ErrGraphCycle), errors.Is(err, workflow.ErrGraphUnreachable), errors.Is(err, workflow.ErrInvalid), errors.Is(err, workflow.ErrInvalidState):
		writeProblem(w, 400, "Bad Request")
	default:
		writeProblem(w, 500, "Internal Server Error")
	}
}
