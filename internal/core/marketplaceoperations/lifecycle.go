package marketplaceoperations

import (
	"context"
	"errors"
	"time"
)

var (
	ErrLifecycleExecutor = errors.New("marketplace operations: lifecycle executor unavailable")
	ErrLifecycleStalled  = errors.New("marketplace operations: lifecycle did not advance")
)

// StepRequest is the provider-neutral input given to the bounded context that
// owns the current stage. It contains canonical references and retry metadata,
// but never a provider payload or credential.
type StepRequest struct {
	Flow           Flow
	Contract       StageContract
	OperationID    string
	IdempotencyKey string
	References     []Reference
	DryRun         bool
}

// StepResult is the normalized result of one local/remote stage execution.
// An executor must return Unknown when a timeout leaves remote acceptance
// ambiguous; the runner then persists that state and stops.
type StepResult struct {
	Outcome    Outcome
	ReasonCode string
	References []Reference
	ObservedAt time.Time
}

func (result StepResult) Validate() error {
	if !result.Outcome.Valid() || result.ObservedAt.IsZero() || result.ObservedAt.Location() != time.UTC {
		return ErrInvalidFlow
	}
	if result.ReasonCode != "" && !flowReferencePattern.MatchString(result.ReasonCode) {
		return ErrInvalidFlow
	}
	if len(result.References) > 64 {
		return ErrInvalidFlow
	}
	for _, reference := range result.References {
		if reference.Validate() != nil {
			return ErrInvalidFlow
		}
	}
	return nil
}

// StageExecutor is the host-side bridge to one canonical bounded context.
// Implementations may call a connector, WMS, EDO or finance adapter, but the
// runner does not know their provider names or transport details.
type StageExecutor interface {
	Execute(context.Context, StepRequest) (StepResult, error)
}

// LifecycleRunner drives one flow until it reaches a terminal/attention
// state. It is deliberately pure with respect to persistence: callers persist
// each returned Flow and command through FlowRepository in their transaction.
// This keeps PostgreSQL, outbox and inbox concerns outside the domain package.
type LifecycleRunner struct {
	Executor StageExecutor
	Now      func() time.Time
	NextID   func(Flow, StageContract) (operationID, idempotencyKey string)
}

// Run advances at most maxSteps stages. A rejected or unknown result is
// persisted in the returned flow and stops execution. A successful result
// without the contract's required reference is rejected before the flow can
// advance. maxSteps bounds a bad adapter and makes worker retries safe.
func (runner LifecycleRunner) Run(ctx context.Context, flow Flow, maxSteps int) (Flow, int, error) {
	if ctx == nil || runner.Executor == nil || flow.Validate() != nil || maxSteps < 1 || maxSteps > len(flowStages) {
		return Flow{}, 0, ErrLifecycleExecutor
	}
	now := runner.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	steps := 0
	for steps < maxSteps && flow.Stage != StageComplete && flow.State != FlowBlocked {
		if err := ctx.Err(); err != nil {
			return flow, steps, err
		}
		contract, ok := ContractFor(flow.Stage)
		if !ok {
			return flow, steps, ErrInvalidTransition
		}
		if runner.NextID == nil {
			return flow, steps, ErrLifecycleExecutor
		}
		operationID, idempotencyKey := runner.NextID(flow, contract)
		request := StepRequest{Flow: flow, Contract: contract, OperationID: operationID, IdempotencyKey: idempotencyKey, References: append([]Reference(nil), flow.References...)}
		if !flowReferencePattern.MatchString(operationID) || !flowReferencePattern.MatchString(idempotencyKey) {
			return flow, steps, ErrInvalidFlow
		}
		result, executeErr := runner.Executor.Execute(ctx, request)
		if executeErr != nil {
			// An adapter error has no safe provider-specific meaning at this
			// boundary. Preserve the possible remote side effect as unknown.
			result = StepResult{Outcome: OutcomeUnknown, ReasonCode: "executor_unknown", ObservedAt: now().UTC()}
		}
		if result.ObservedAt.IsZero() {
			result.ObservedAt = now().UTC()
		}
		if result.Validate() != nil {
			return flow, steps, ErrInvalidFlow
		}
		command := Command{OperationID: operationID, IdempotencyKey: idempotencyKey, Stage: flow.Stage, Outcome: result.Outcome, ReasonCode: result.ReasonCode, References: result.References, OccurredAt: result.ObservedAt}
		updated, _, applyErr := Apply(flow, command)
		if applyErr != nil {
			return flow, steps, applyErr
		}
		if executeErr != nil {
			return updated, steps + 1, executeErr
		}
		flow = updated
		steps++
		if result.Outcome != OutcomeSucceeded {
			return flow, steps, nil
		}
	}
	if flow.Stage != StageComplete && flow.State != FlowBlocked && steps == maxSteps {
		return flow, steps, ErrLifecycleStalled
	}
	return flow, steps, nil
}
