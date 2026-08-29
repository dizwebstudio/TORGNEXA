// Package workflowengine executes the bounded workflow plan through typed host
// adapters. It never evaluates user code or performs provider transport.
package workflowengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/workflow"
)

var (
	ErrAdapterUnavailable = errors.New("workflow engine: action adapter unavailable")
	ErrRetryable          = errors.New("workflow engine: retryable action failure")
	ErrApprovalRequired   = errors.New("workflow engine: approval required")
)

type Store interface {
	GetRun(context.Context, workflow.Scope, string) (workflow.Run, error)
	Version(context.Context, workflow.Scope, string, int64) (workflow.WorkflowVersion, error)
	UpdateRun(context.Context, workflow.Scope, workflow.Run, int64) (workflow.Run, error)
	Step(context.Context, workflow.Scope, string, string) (workflow.StepRun, error)
	UpsertStep(context.Context, workflow.Scope, workflow.StepRun, int64) (workflow.StepRun, error)
	AppendEvidence(context.Context, workflow.Scope, string, string, string, int, workflow.StepStatus, string, string, time.Time) error
	ReleaseRun(context.Context, workflow.Scope, string, string, workflow.RunStatus, time.Time, string) error
}

// ActionRequest is a typed, bounded adapter input. It deliberately contains
// no event payload or credential material.
type ActionRequest struct {
	Scope           workflow.Scope
	WorkflowID      string
	WorkflowVersion int64
	RunID           string
	Node            workflow.Node
	Attempt         int
	IdempotencyKey  string
}

// Adapter is the host-mediated boundary for one allowlisted action.
type Adapter interface {
	Execute(context.Context, ActionRequest) error
}

type AdapterFunc func(context.Context, ActionRequest) error

func (f AdapterFunc) Execute(ctx context.Context, request ActionRequest) error {
	return f(ctx, request)
}

// Registry is an immutable allowlist of typed adapters.
type Registry struct{ adapters map[string]Adapter }

func NewRegistry(adapters map[string]Adapter) (*Registry, error) {
	copy := make(map[string]Adapter, len(adapters))
	for name, adapter := range adapters {
		if !workflow.AllowedAction(name) || adapter == nil {
			return nil, workflow.ErrInvalid
		}
		copy[name] = adapter
	}
	return &Registry{adapters: copy}, nil
}

type Engine struct {
	store    Store
	registry *Registry
	now      func() time.Time
}

func New(store Store, registry *Registry) (*Engine, error) {
	if store == nil || registry == nil {
		return nil, workflow.ErrInvalid
	}
	return &Engine{store: store, registry: registry, now: func() time.Time { return time.Now().UTC() }}, nil
}

// Execute runs one claimed run. Every side effect is represented by a step
// evidence row; a missing adapter fails before any remote call.
func (e *Engine) Execute(ctx context.Context, scope workflow.Scope, runID, leaseToken string) error {
	if ctx == nil || e == nil || e.store == nil || e.registry == nil || !scope.Valid() || runID == "" || leaseToken == "" {
		return workflow.ErrInvalid
	}
	run, err := e.store.GetRun(ctx, scope, runID)
	if err != nil {
		return err
	}
	version, err := e.store.Version(ctx, scope, run.WorkflowID, run.WorkflowVersion)
	if err != nil {
		return err
	}
	plan, err := workflow.Compile(version.Definition)
	if err != nil || plan.Digest != version.PlanDigest {
		return workflow.ErrInvalid
	}
	now := e.now().UTC()
	run.Status = workflow.RunRunning
	run.StartedAt = &now
	run.AttemptCount++
	if _, err := e.store.UpdateRun(ctx, scope, run, run.Version); err != nil {
		return err
	}
	nodes := make(map[string]workflow.Node, len(version.Definition.Nodes))
	for _, node := range version.Definition.Nodes {
		nodes[node.ID] = node
	}
	for _, nodeID := range plan.NodeIDs {
		node := nodes[nodeID]
		step, stepErr := e.store.Step(ctx, scope, run.ID, node.ID)
		existing := stepErr == nil
		if stepErr != nil && !errors.Is(stepErr, workflow.ErrNotFound) {
			return stepErr
		}
		if existing && step.Status == workflow.StepCompleted {
			continue
		}
		if !existing {
			step = workflow.StepRun{ID: fmt.Sprintf("step_%s_%s", run.ID, node.ID), RunID: run.ID, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), NodeID: node.ID, Status: workflow.StepQueued, Version: 1}
		}
		step.Status = workflow.StepRunning
		step.AttemptCount++
		started := e.now().UTC()
		step.StartedAt = &started
		updated, err := e.store.UpsertStep(ctx, scope, step, func() int64 {
			if existing {
				return step.Version
			}
			return 0
		}())
		if err != nil {
			return err
		}
		step = updated
		var actionErr error
		spec, isAction := workflow.Action(node.Action)
		switch node.Kind {
		case workflow.NodeCondition, workflow.NodeDelay:
			// v1 conditions are evaluated against the trigger reference only; no
			// network or arbitrary expression evaluation is permitted.
		case workflow.NodeAction, workflow.NodeApproval:
			if !isAction || !spec.DryRun && spec.Risk == workflow.RiskRead {
				actionErr = ErrAdapterUnavailable
				break
			}
			adapter := e.registry.adapters[node.Action]
			if adapter == nil {
				actionErr = ErrAdapterUnavailable
				break
			}
			actionErr = adapter.Execute(ctx, ActionRequest{Scope: scope, WorkflowID: run.WorkflowID, WorkflowVersion: run.WorkflowVersion, RunID: run.ID, Node: node, Attempt: step.AttemptCount, IdempotencyKey: run.IdempotencyKey})
		default:
			actionErr = workflow.ErrInvalid
		}
		outcome := workflow.StepCompleted
		errorCode := ""
		runStatus := workflow.RunCompleted
		if actionErr != nil {
			outcome = workflow.StepFailed
			errorCode = classify(actionErr)
			runStatus = workflow.RunFailed
			if errors.Is(actionErr, ErrRetryable) {
				outcome = workflow.StepWaitingRetry
				runStatus = workflow.RunWaitingRetry
			}
			if errors.Is(actionErr, ErrApprovalRequired) {
				outcome = workflow.StepWaitingApproval
				runStatus = workflow.RunWaitingApproval
			}
		}
		completed := e.now().UTC()
		step.Status = outcome
		step.ErrorCode = errorCode
		step.CompletedAt = &completed
		if outcome == workflow.StepCompleted {
			step.OutputDigest = digest(node.Action + ":" + node.ID + ":" + run.ID)
		}
		if _, err := e.store.UpsertStep(ctx, scope, step, step.Version); err != nil {
			return err
		}
		if err := e.store.AppendEvidence(ctx, scope, fmt.Sprintf("evidence_%s_%s_%d", run.ID, node.ID, step.AttemptCount), run.ID, node.ID, step.AttemptCount, outcome, step.OutputDigest, errorCode, completed); err != nil {
			return err
		}
		if actionErr != nil {
			next := completed.Add(time.Minute)
			if errors.Is(actionErr, ErrApprovalRequired) {
				next = completed.Add(24 * time.Hour)
			}
			if err := e.store.ReleaseRun(ctx, scope, run.ID, leaseToken, runStatus, next, errorCode); err != nil {
				return err
			}
			return actionErr
		}
	}
	return e.store.ReleaseRun(ctx, scope, run.ID, leaseToken, workflow.RunCompleted, e.now().UTC(), "")
}

func classify(err error) string {
	switch {
	case errors.Is(err, ErrAdapterUnavailable):
		return "adapter_unavailable"
	case errors.Is(err, ErrRetryable):
		return "temporary_failure"
	case errors.Is(err, ErrApprovalRequired):
		return "approval_required"
	default:
		return "action_failed"
	}
}
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
