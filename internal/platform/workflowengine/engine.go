// Package workflowengine executes the bounded workflow plan through typed host
// adapters. It never evaluates user code or performs provider transport.
package workflowengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
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
	if run.Status != workflow.RunQueued && run.Status != workflow.RunRunning && run.Status != workflow.RunWaitingRetry && run.Status != workflow.RunWaitingApproval {
		return workflow.ErrInvalidState
	}
	version, err := e.store.Version(ctx, scope, run.WorkflowID, run.WorkflowVersion)
	if err != nil {
		return err
	}
	if version.PublishedAt == nil {
		_ = e.store.ReleaseRun(ctx, scope, run.ID, leaseToken, workflow.RunFailed, e.now().UTC(), "version_unpublished")
		return workflow.ErrInvalidState
	}
	plan, err := workflow.Compile(version.Definition)
	if err != nil || plan.Digest != version.PlanDigest {
		_ = e.store.ReleaseRun(ctx, scope, run.ID, leaseToken, workflow.RunFailed, e.now().UTC(), "plan_invalid")
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
	incoming := make(map[string][]workflow.Edge, len(version.Definition.Nodes))
	for _, node := range version.Definition.Nodes {
		nodes[node.ID] = node
	}
	for _, edge := range version.Definition.Edges {
		incoming[edge.To] = append(incoming[edge.To], edge)
	}
	nodeResults := make(map[string]bool, len(nodes))
	for _, nodeID := range plan.NodeIDs {
		node := nodes[nodeID]
		step, stepErr := e.store.Step(ctx, scope, run.ID, node.ID)
		existing := stepErr == nil
		if stepErr != nil && !errors.Is(stepErr, workflow.ErrNotFound) {
			return stepErr
		}
		if existing && step.Status == workflow.StepCompleted {
			nodeResults[node.ID] = node.Kind != workflow.NodeCondition || conditionResult(step.OutputDigest)
			continue
		}
		if existing && step.Status == workflow.StepSkipped {
			nodeResults[node.ID] = false
			continue
		}
		previousStatus := step.Status
		previousStartedAt := step.StartedAt
		if !existing {
			step = workflow.StepRun{ID: "step_" + digest(run.ID + ":" + node.ID)[:32], RunID: run.ID, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), NodeID: node.ID, Status: workflow.StepQueued, Version: 1}
		}
		runnable := true
		if parents := incoming[node.ID]; len(parents) > 0 {
			runnable = false
			for _, edge := range parents {
				if nodeResults[edge.From] && edge.Condition != "false" {
					runnable = true
					break
				}
			}
		}
		if !runnable {
			step.Status = workflow.StepSkipped
			if step.AttemptCount < 1 {
				step.AttemptCount = 1
			}
			step.ErrorCode = ""
			step.CompletedAt = nil
			if _, err := e.store.UpsertStep(ctx, scope, step, func() int64 {
				if existing {
					return step.Version
				}
				return 0
			}()); err != nil {
				return err
			}
			observed := e.now().UTC()
			if err := e.store.AppendEvidence(ctx, scope, "evidence_"+digest(run.ID + ":" + node.ID + ":skipped")[:40], run.ID, node.ID, step.AttemptCount, workflow.StepSkipped, "", "", observed); err != nil {
				return err
			}
			nodeResults[node.ID] = false
			continue
		}
		step.Status = workflow.StepRunning
		step.AttemptCount++
		started := e.now().UTC()
		step.StartedAt = &started
		if node.Kind == workflow.NodeDelay && previousStartedAt != nil {
			step.StartedAt = previousStartedAt
		}
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
		nodeResult := true
		spec, isAction := workflow.Action(node.Action)
		switch node.Kind {
		case workflow.NodeCondition:
			nodeResult, actionErr = evaluateCondition(node.Config)
		case workflow.NodeDelay:
			delay, delayErr := parseDelay(node.Config)
			if delayErr != nil {
				actionErr = delayErr
				break
			}
			if delay > 0 && (previousStatus != workflow.StepWaitingRetry || previousStartedAt == nil || e.now().UTC().Before(previousStartedAt.Add(delay))) {
				actionErr = ErrRetryable
			}
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
				if step.AttemptCount < workflow.MaxStepAttempts {
					outcome = workflow.StepWaitingRetry
					runStatus = workflow.RunWaitingRetry
				} else {
					// A retryable dependency must not create an unbounded loop or
					// exhaust the durable attempt column. Leave a terminal,
					// operator-visible evidence record after the bounded budget.
					errorCode = "retry_exhausted"
				}
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
		if outcome == workflow.StepWaitingRetry || outcome == workflow.StepWaitingApproval {
			step.CompletedAt = nil
		}
		if outcome == workflow.StepCompleted {
			if node.Kind == workflow.NodeCondition {
				if nodeResult {
					step.OutputDigest = digest("workflow.condition:true")
				} else {
					step.OutputDigest = digest("workflow.condition:false")
				}
			} else {
				step.OutputDigest = digest(node.Action + ":" + node.ID + ":" + run.ID)
			}
		}
		if _, err := e.store.UpsertStep(ctx, scope, step, step.Version); err != nil {
			return err
		}
		if err := e.store.AppendEvidence(ctx, scope, "evidence_"+digest(run.ID + ":" + node.ID + ":" + strconv.Itoa(step.AttemptCount) + ":" + string(outcome))[:40], run.ID, node.ID, step.AttemptCount, outcome, step.OutputDigest, errorCode, completed); err != nil {
			return err
		}
		nodeResults[node.ID] = actionErr == nil && nodeResult
		if actionErr != nil {
			next := completed.Add(retryDelay(step.AttemptCount))
			if errors.Is(actionErr, ErrApprovalRequired) {
				next = completed.Add(time.Minute)
			}
			if err := e.store.ReleaseRun(ctx, scope, run.ID, leaseToken, runStatus, next, errorCode); err != nil {
				return err
			}
			return actionErr
		}
	}
	return e.store.ReleaseRun(ctx, scope, run.ID, leaseToken, workflow.RunCompleted, e.now().UTC(), "")
}

func conditionResult(outputDigest string) bool {
	// A completed condition records true/false as a digest of the finite
	// result.  Older records without that marker remain fail-open for backwards
	// compatibility with the original v1 executor.
	return outputDigest != digest("workflow.condition:false")
}

func evaluateCondition(config []byte) (bool, error) {
	if len(config) == 0 {
		return true, nil
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(config, &values); err != nil {
		return false, workflow.ErrInvalid
	}
	for _, key := range []string{"result", "value", "enabled"} {
		raw, ok := values[key]
		if !ok {
			continue
		}
		var flag bool
		if err := json.Unmarshal(raw, &flag); err == nil {
			return flag, nil
		}
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			switch text {
			case "true":
				return true, nil
			case "false":
				return false, nil
			}
		}
		return false, workflow.ErrInvalid
	}
	return true, nil
}

func parseDelay(config []byte) (time.Duration, error) {
	if len(config) == 0 {
		return 0, nil
	}
	var values struct {
		Seconds int `json:"seconds"`
	}
	if err := json.Unmarshal(config, &values); err != nil || values.Seconds < 0 || values.Seconds > 24*60*60 {
		return 0, workflow.ErrInvalid
	}
	return time.Duration(values.Seconds) * time.Second, nil
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

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	// Deterministic bounded jitter avoids a thundering herd after a shared
	// outage while keeping replay/recovery reproducible in tests and evidence.
	base := time.Minute
	for i := 1; i < attempt && i < 6; i++ {
		base *= 2
	}
	if base > 32*time.Minute {
		base = 32 * time.Minute
	}
	jitter := time.Duration((attempt*37)%25) * time.Second
	return base + jitter
}
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
