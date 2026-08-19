package api

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
)

const ApprovalsPath = "/api/v1/approvals"
const ApprovalPoliciesPath = "/api/v1/approval-policies"

type approvalReader interface {
	ListRequests(context.Context, tenancy.Scope, int) ([]approval.Request, error)
}
type approvalWorkflow interface {
	approvalReader
	InstallPolicy(context.Context, tenancy.Scope, approval.Policy, approval.Mutation) error
	CreateRequest(context.Context, tenancy.Scope, string, string, approval.RequestCommand) (approval.Request, error)
	Decide(context.Context, tenancy.Scope, approval.DecideCommand) (approval.Request, error)
}
type approvalPolicyStore interface {
	ListPolicies(context.Context, tenancy.Scope, int) ([]approval.Policy, error)
	RetirePolicy(context.Context, tenancy.Scope, string, int64, approval.Mutation) error
}
type approvalExecutionWorkflow interface {
	Request(context.Context, tenancy.Scope, string) (approval.Request, error)
	BeginExecution(context.Context, tenancy.Scope, approval.TransitionCommand) (approval.Request, error)
	CompleteExecution(context.Context, tenancy.Scope, approval.TransitionCommand, bool) (approval.Request, error)
}
type approvalSensitiveExecutor interface {
	DeleteDemoOrders(context.Context, tenancy.Scope) (int, error)
}
type approvalView struct {
	ID           string                `json:"id"`
	RequesterID  string                `json:"requester_id"`
	Action       string                `json:"action"`
	ResourceType string                `json:"resource_type"`
	ResourceID   string                `json:"resource_id"`
	Risk         approval.RiskClass    `json:"risk"`
	State        approval.RequestState `json:"state"`
	CurrentStage uint16                `json:"current_stage"`
	ExpiresAt    time.Time             `json:"expires_at"`
	RequestedAt  time.Time             `json:"requested_at"`
	Version      int64                 `json:"version"`
}
type approvalPolicyView struct {
	ID                   string             `json:"id"`
	Name                 string             `json:"name"`
	Action               string             `json:"action"`
	ResourceType         string             `json:"resource_type"`
	MinimumRisk          approval.RiskClass `json:"minimum_risk"`
	RequestTTLMinutes    int64              `json:"request_ttl_minutes"`
	EscalateAfterMinutes int64              `json:"escalate_after_minutes"`
	SeparationOfDuties   bool               `json:"separation_of_duties"`
	RequiredApprovals    uint16             `json:"required_approvals"`
	EligibleScope        string             `json:"eligible_scope"`
	Active               bool               `json:"active"`
	Version              int64              `json:"version"`
}

func newApprovalRoutes(repository approvalWorkflow, executors ...approvalSensitiveExecutor) []ProtectedRoute {
	var executor approvalSensitiveExecutor
	if len(executors) > 0 {
		executor = executors[0]
	}
	return []ProtectedRoute{{Method: http.MethodGet, Path: ApprovalPoliciesPath, Permission: "approvals.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store, ok := repository.(approvalPolicyStore)
		scope, scoped := ScopeFromContext(r.Context())
		if !ok || !scoped {
			writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
			return
		}
		items, err := store.ListPolicies(r.Context(), scope, 100)
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		views := make([]approvalPolicyView, 0, len(items))
		for _, item := range items {
			views = append(views, toApprovalPolicyView(item))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": views})
	})}, {Method: http.MethodPost, Path: ApprovalPoliciesPath, Permission: "approvals.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := ScopeFromContext(r.Context())
		principal, principalOK := PrincipalFromContext(r.Context())
		correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		var input struct {
			ID                   string `json:"id"`
			Name                 string `json:"name"`
			Action               string `json:"action"`
			ResourceType         string `json:"resource_type"`
			MinimumRisk          string `json:"minimum_risk"`
			EligibleScope        string `json:"eligible_scope"`
			Version              int64  `json:"version"`
			RequestTTLMinutes    int64  `json:"request_ttl_minutes"`
			EscalateAfterMinutes int64  `json:"escalate_after_minutes"`
			RequiredApprovals    uint16 `json:"required_approvals"`
			SeparationOfDuties   bool   `json:"separation_of_duties"`
		}
		if !ok || !principalOK || correlation == "" || decodeStrictJSON(r, &input) != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		policy := approval.Policy{ID: input.ID, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), Name: input.Name, Action: input.Action, ResourceType: input.ResourceType, MinimumRisk: approval.RiskClass(input.MinimumRisk), Version: input.Version, RequestTTL: time.Duration(input.RequestTTLMinutes) * time.Minute, EscalateAfter: time.Duration(input.EscalateAfterMinutes) * time.Minute, SeparationOfDuties: input.SeparationOfDuties, Stages: []approval.Stage{{Number: 1, Name: "workspace_approvers", RequiredApprovals: input.RequiredApprovals, EligibleScopes: []string{input.EligibleScope}}}, Active: true}
		if err := repository.InstallPolicy(r.Context(), scope, policy, newApprovalMutation(principal.Subject, correlation, time.Now().UTC())); err != nil {
			if errors.Is(err, approval.ErrConflict) {
				writeProblem(w, http.StatusConflict, "Conflict")
			} else {
				writeProblem(w, http.StatusBadRequest, "Bad Request")
			}
			return
		}
		writeJSON(w, http.StatusCreated, toApprovalPolicyView(policy))
	})}, {Method: http.MethodDelete, Path: ApprovalPoliciesPath + "/", PathPrefix: true, Permission: "approvals.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store, supported := repository.(approvalPolicyStore)
		scope, ok := ScopeFromContext(r.Context())
		principal, principalOK := PrincipalFromContext(r.Context())
		correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		id := strings.TrimPrefix(r.URL.Path, ApprovalPoliciesPath+"/")
		version, parseErr := strconv.ParseInt(r.URL.Query().Get("expected_version"), 10, 64)
		if !supported || !ok || !principalOK || correlation == "" || id == "" || strings.Contains(id, "/") || parseErr != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		if err := store.RetirePolicy(r.Context(), scope, id, version, newApprovalMutation(principal.Subject, correlation, time.Now().UTC())); err != nil {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})}, {Method: http.MethodPost, Path: ApprovalsPath + "/requests", Permission: "approvals.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := ScopeFromContext(r.Context())
		principal, principalOK := PrincipalFromContext(r.Context())
		correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		var input struct {
			Action       string             `json:"action"`
			ResourceType string             `json:"resource_type"`
			ResourceID   string             `json:"resource_id"`
			Risk         approval.RiskClass `json:"risk"`
		}
		if !ok || !principalOK || correlation == "" || decodeStrictJSON(r, &input) != nil || input.Action != "demo.dataset.delete" || input.ResourceType != "demo_dataset" {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		if input.ResourceID == "" {
			input.ResourceID = scope.WorkspaceID().String()
		}
		if input.ResourceID != scope.WorkspaceID().String() {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		request, err := repository.CreateRequest(r.Context(), scope, input.Action, input.ResourceType, approval.RequestCommand{RequestID: newApprovalID(), ResourceID: input.ResourceID, Risk: input.Risk, Mutation: newApprovalMutation(principal.Subject, correlation, time.Now().UTC())})
		if err != nil {
			writeProblem(w, http.StatusConflict, "Matching active policy required")
			return
		}
		writeJSON(w, http.StatusCreated, toApprovalView(request))
	})}, {Method: http.MethodPost, Path: ApprovalsPath + "/demo", Permission: "approvals.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := ScopeFromContext(r.Context())
		principal, principalOK := PrincipalFromContext(r.Context())
		correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if !ok || !principalOK || correlation == "" {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		now := time.Now().UTC()
		policyID, requestID := newApprovalID(), newApprovalID()
		policy := approval.Policy{ID: policyID, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), Name: "demo_sensitive_publication", Action: "demo.catalog.publish", ResourceType: "product", MinimumRisk: approval.RiskWriteSensitive, Version: 1, RequestTTL: 24 * time.Hour, EscalateAfter: time.Hour, Stages: []approval.Stage{{Number: 1, Name: "administrator", RequiredApprovals: 1, EligibleScopes: []string{"approval.demo"}}}, Active: true}
		if err := repository.InstallPolicy(r.Context(), scope, policy, newApprovalMutation(principal.Subject, correlation, now)); err != nil {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		request, err := repository.CreateRequest(r.Context(), scope, policy.Action, policy.ResourceType, approval.RequestCommand{RequestID: requestID, ResourceID: "DEMO-PRODUCT", Risk: approval.RiskWriteSensitive, Mutation: newApprovalMutation(principal.Subject, correlation+":request", now.Add(time.Millisecond))})
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		writeJSON(w, http.StatusCreated, toApprovalView(request))
	})}, {Method: http.MethodPost, Path: ApprovalsPath + "/", PathPrefix: true, Permission: "approvals.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := ScopeFromContext(r.Context())
		principal, principalOK := PrincipalFromContext(r.Context())
		correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		path := strings.TrimPrefix(r.URL.Path, ApprovalsPath+"/")
		if requestID, execute := strings.CutSuffix(path, "/execute"); execute {
			workflow, supported := repository.(approvalExecutionWorkflow)
			if !ok || !principalOK || !supported || executor == nil || strings.Contains(requestID, "/") || correlation == "" {
				writeProblem(w, http.StatusBadRequest, "Bad Request")
				return
			}
			current, err := workflow.Request(r.Context(), scope, requestID)
			if err != nil || current.State != approval.StateApproved || current.Action != "demo.dataset.delete" || current.ResourceType != "demo_dataset" || current.ResourceID != scope.WorkspaceID().String() {
				writeProblem(w, http.StatusConflict, "Approved matching request required")
				return
			}
			executing, err := workflow.BeginExecution(r.Context(), scope, approval.TransitionCommand{RequestID: requestID, ExpectedVersion: current.Version, Mutation: newApprovalMutation(principal.Subject, correlation, time.Now().UTC())})
			if err != nil {
				writeProblem(w, http.StatusConflict, "Conflict")
				return
			}
			if _, err = approval.Grant(executing, current.Action, current.ResourceType, current.ResourceID); err != nil {
				writeProblem(w, http.StatusForbidden, "Forbidden")
				return
			}
			count, operationErr := executor.DeleteDemoOrders(r.Context(), scope)
			failure := ""
			if operationErr != nil {
				failure = "demo_delete_failed"
			}
			completed, transitionErr := workflow.CompleteExecution(r.Context(), scope, approval.TransitionCommand{RequestID: requestID, ExpectedVersion: executing.Version, FailureCode: failure, Mutation: newApprovalMutation(principal.Subject, correlation+":complete", time.Now().UTC())}, operationErr == nil)
			if operationErr != nil || transitionErr != nil {
				writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"request": toApprovalView(completed), "deleted": count})
			return
		}
		requestID, found := strings.CutSuffix(path, "/decision")
		if !ok || !principalOK || !found || strings.Contains(requestID, "/") || correlation == "" {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		var input struct {
			Vote            approval.Vote `json:"vote"`
			ExpectedVersion int64         `json:"expected_version"`
		}
		if decodeStrictJSON(r, &input) != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		request, err := repository.Decide(r.Context(), scope, approval.DecideCommand{RequestID: requestID, DecisionID: newApprovalID(), ExpectedVersion: input.ExpectedVersion, Actor: approval.Actor{ID: principal.Subject, Scopes: []string{"approval.demo"}}, Vote: input.Vote, Comment: "Решение принято в интерфейсе TORGNEXA", Mutation: newApprovalMutation(principal.Subject, correlation, time.Now().UTC())})
		if errors.Is(err, approval.ErrInvalid) || errors.Is(err, approval.ErrNotEligible) || errors.Is(err, approval.ErrInvalidState) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		if err != nil {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		writeJSON(w, http.StatusOK, toApprovalView(request))
	})}, {Method: http.MethodGet, Path: ApprovalsPath, Permission: "approvals.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := ScopeFromContext(r.Context())
		if !ok || repository == nil {
			writeProblem(w, http.StatusForbidden, "Forbidden")
			return
		}
		items, err := repository.ListRequests(r.Context(), scope, 100)
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		views := make([]approvalView, 0, len(items))
		for _, v := range items {
			views = append(views, approvalView{v.ID, v.RequesterID, v.Action, v.ResourceType, v.ResourceID, v.Risk, v.State, v.CurrentStage, v.ExpiresAt, v.RequestedAt, v.Version})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": views})
	})}}
}

func toApprovalView(v approval.Request) approvalView {
	return approvalView{v.ID, v.RequesterID, v.Action, v.ResourceType, v.ResourceID, v.Risk, v.State, v.CurrentStage, v.ExpiresAt, v.RequestedAt, v.Version}
}
func toApprovalPolicyView(v approval.Policy) approvalPolicyView {
	view := approvalPolicyView{ID: v.ID, Name: v.Name, Action: v.Action, ResourceType: v.ResourceType, MinimumRisk: v.MinimumRisk, RequestTTLMinutes: int64(v.RequestTTL / time.Minute), EscalateAfterMinutes: int64(v.EscalateAfter / time.Minute), SeparationOfDuties: v.SeparationOfDuties, Active: v.Active, Version: v.Version}
	if len(v.Stages) > 0 {
		view.RequiredApprovals = v.Stages[0].RequiredApprovals
		if len(v.Stages[0].EligibleScopes) > 0 {
			view.EligibleScope = v.Stages[0].EligibleScopes[0]
		}
	}
	return view
}
func newApprovalMutation(actor, correlation string, occurred time.Time) approval.Mutation {
	return approval.Mutation{AuditID: newApprovalID(), EventID: newApprovalID(), ActorID: actor, Source: "api", CorrelationID: correlation, OccurredAt: occurred.UTC()}
}
func newApprovalID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return ""
	}
	millis := uint64(time.Now().UnixMilli())
	value[0], value[1], value[2], value[3], value[4], value[5] = byte(millis>>40), byte(millis>>32), byte(millis>>24), byte(millis>>16), byte(millis>>8), byte(millis)
	value[6], value[8] = (value[6]&0x0f)|0x70, (value[8]&0x3f)|0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}
