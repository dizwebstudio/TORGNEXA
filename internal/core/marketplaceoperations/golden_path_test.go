package marketplaceoperations

import (
	"context"
	"testing"
	"time"
)

type goldenPathExecutor struct {
	calls map[string]int
}

func (executor *goldenPathExecutor) Execute(_ context.Context, request StepRequest) (StepResult, error) {
	if executor.calls == nil {
		executor.calls = make(map[string]int)
	}
	executor.calls[request.IdempotencyKey]++
	return StepResult{
		Outcome: OutcomeSucceeded,
		References: []Reference{{Kind: request.Contract.RequiredReferenceKinds[0], ID: "record-" + string(request.Flow.Stage)}},
		ObservedAt: time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC).Add(time.Duration(len(executor.calls)) * time.Minute),
	}, nil
}

func TestGoldenPathRunsOrderThroughRefundAndReconciliation(t *testing.T) {
	at := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
	flow, err := NewAtStage("golden-flow", "org-1", "workspace-1", "account-1", StageOrder, []Reference{{Kind: "order", ID: "order-1"}}, at)
	if err != nil {
		t.Fatal(err)
	}
	executor := &goldenPathExecutor{}
	runner := LifecycleRunner{Executor: executor, NextID: lifecycleID}
	updated, steps, err := runner.Run(context.Background(), flow, len(GoldenPathStages()))
	if err != nil {
		t.Fatalf("golden path failed: %v", err)
	}
	if updated.Stage != StageComplete || updated.State != FlowComplete || steps != len(GoldenPathStages()) {
		t.Fatalf("golden path incomplete: stage=%s state=%s steps=%d", updated.Stage, updated.State, steps)
	}
	for _, stage := range GoldenPathStages() {
		if executor.calls["idem-"+string(stage)] != 1 {
			t.Fatalf("stage %s was not executed exactly once: calls=%v", stage, executor.calls)
		}
	}
	if len(updated.References) != len(GoldenPathStages()) {
		t.Fatalf("canonical lineage is incomplete: %+v", updated.References)
	}
}

