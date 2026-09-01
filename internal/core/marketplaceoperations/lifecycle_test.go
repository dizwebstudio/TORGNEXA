package marketplaceoperations

import (
	"context"
	"errors"
	"testing"
	"time"
)

type lifecycleExecutor struct {
	results  []StepResult
	requests []StepRequest
}

func (executor *lifecycleExecutor) Execute(_ context.Context, request StepRequest) (StepResult, error) {
	executor.requests = append(executor.requests, request)
	if len(executor.results) == 0 {
		return StepResult{}, errors.New("remote timeout")
	}
	result := executor.results[0]
	executor.results = executor.results[1:]
	return result, nil
}

func lifecycleID(_ Flow, contract StageContract) (string, string) {
	return "op-" + string(contract.Stage), "idem-" + string(contract.Stage)
}

func lifecycleFlow(t *testing.T) Flow {
	t.Helper()
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	flow, err := New("flow-1", "org-1", "ws-1", "account-1", at)
	if err != nil {
		t.Fatal(err)
	}
	return flow
}

func TestLifecycleRunnerStopsOnUnknownAndDoesNotAdvance(t *testing.T) {
	executor := &lifecycleExecutor{results: []StepResult{
		{Outcome: OutcomeSucceeded, References: []Reference{{Kind: "account", ID: "account-1"}}, ObservedAt: time.Date(2026, 9, 1, 10, 1, 0, 0, time.UTC)},
		{Outcome: OutcomeUnknown, ReasonCode: "remote_timeout", ObservedAt: time.Date(2026, 9, 1, 10, 2, 0, 0, time.UTC)},
	}}
	runner := LifecycleRunner{Executor: executor, Now: func() time.Time { return time.Date(2026, 9, 1, 10, 2, 0, 0, time.UTC) }, NextID: lifecycleID}
	flow, steps, err := runner.Run(context.Background(), lifecycleFlow(t), 2)
	if err != nil {
		t.Fatalf("unknown remote outcome should be persisted without retry: flow=%#v steps=%d err=%v", flow, steps, err)
	}
	if flow.Stage != StageProduct || flow.State != FlowUnknown || steps != 2 {
		t.Fatalf("unknown stage was not persisted safely: stage=%s state=%s steps=%d", flow.Stage, flow.State, steps)
	}
}

func TestLifecycleRunnerRequiresTypedReferenceBeforeAdvance(t *testing.T) {
	executor := &lifecycleExecutor{results: []StepResult{{Outcome: OutcomeSucceeded, ObservedAt: time.Date(2026, 9, 1, 10, 1, 0, 0, time.UTC)}}}
	runner := LifecycleRunner{Executor: executor, NextID: lifecycleID}
	flow, steps, err := runner.Run(context.Background(), lifecycleFlow(t), 1)
	if !errors.Is(err, ErrMissingReference) || steps != 0 || flow.Stage != StageAccount {
		t.Fatalf("missing account reference should prevent transition: flow=%#v steps=%d err=%v", flow, steps, err)
	}
}

func TestLifecycleRunnerCompletesSyntheticScenario(t *testing.T) {
	at := time.Date(2026, 9, 1, 10, 1, 0, 0, time.UTC)
	results := make([]StepResult, 0, len(flowStages)-1)
	for _, contract := range StageContracts() {
		results = append(results, StepResult{Outcome: OutcomeSucceeded, References: []Reference{{Kind: contract.RequiredReferenceKinds[0], ID: contract.RequiredReferenceKinds[0] + "-1"}}, ObservedAt: at})
		at = at.Add(time.Minute)
	}
	executor := &lifecycleExecutor{results: results}
	runner := LifecycleRunner{Executor: executor, NextID: lifecycleID}
	flow, steps, err := runner.Run(context.Background(), lifecycleFlow(t), len(flowStages))
	if err != nil {
		t.Fatalf("synthetic lifecycle failed: %v", err)
	}
	if flow.Stage != StageComplete || flow.State != FlowComplete || steps != len(flowStages)-1 {
		t.Fatalf("synthetic lifecycle incomplete: stage=%s state=%s steps=%d", flow.Stage, flow.State, steps)
	}
	if len(executor.requests) != steps {
		t.Fatalf("executor calls=%d, steps=%d", len(executor.requests), steps)
	}
}
