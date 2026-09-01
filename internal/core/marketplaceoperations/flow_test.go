package marketplaceoperations

import (
	"errors"
	"testing"
	"time"
)

func flowCommand(stage FlowStage, operationID, key string, outcome Outcome, at time.Time, references ...Reference) Command {
	return Command{Stage: stage, OperationID: operationID, IdempotencyKey: key, Outcome: outcome, References: references, OccurredAt: at}
}

func TestFlowAdvancesFullMarketplaceScenarioAndDeduplicatesRetry(t *testing.T) {
	at := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	flow, err := New("flow-1", "org-1", "workspace-1", "account-1", at)
	if err != nil {
		t.Fatal(err)
	}
	stages := append([]FlowStage(nil), flowStages[:len(flowStages)-1]...)
	referenceKinds := map[FlowStage]string{
		StageAccount: "account", StageProduct: "product", StagePublication: "publication", StagePricing: "price",
		StageInventory: "inventory", StageOrder: "order", StageReservation: "reservation", StagePickPack: "wms_task",
		StageShipment: "shipment", StageReturn: "return", StageSettlement: "settlement", StageProfitability: "pnl", StageReconciliation: "reconciliation",
	}
	for index, stage := range stages {
		command := flowCommand(stage, "operation-"+string(stage), "key-"+string(stage), OutcomeSucceeded, at.Add(time.Duration(index+1)*time.Minute), Reference{Kind: referenceKinds[stage], ID: "record-" + string(stage)})
		next, duplicate, applyErr := Apply(flow, command)
		if applyErr != nil || duplicate {
			t.Fatalf("stage %s apply: duplicate=%v err=%v", stage, duplicate, applyErr)
		}
		flow = next
		if index == 0 {
			retry, duplicate, applyErr := Apply(flow, command)
			if applyErr != nil || !duplicate || retry.Version != flow.Version {
				t.Fatalf("duplicate retry: duplicate=%v err=%v retry=%+v flow=%+v", duplicate, applyErr, retry, flow)
			}
		}
	}
	if flow.Stage != StageComplete || flow.State != FlowComplete || len(flow.References) != len(stages) {
		t.Fatalf("flow did not complete: %+v", flow)
	}
}

func TestFlowKeepsUnknownAtStageAndRejectsOutOfOrderCommands(t *testing.T) {
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	flow, err := New("flow-2", "org-1", "workspace-1", "account-1", at)
	if err != nil {
		t.Fatal(err)
	}
	flow, _, err = Apply(flow, flowCommand(StageAccount, "operation-1", "key-1", OutcomeUnknown, at.Add(time.Minute)))
	if err != nil || flow.Stage != StageAccount || flow.State != FlowUnknown {
		t.Fatalf("unknown outcome changed stage: flow=%+v err=%v", flow, err)
	}
	if _, _, err = Apply(flow, flowCommand(StageProduct, "operation-2", "key-2", OutcomeSucceeded, at.Add(2*time.Minute))); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("out-of-order command error=%v, want %v", err, ErrInvalidTransition)
	}
	flow, _, err = Apply(flow, flowCommand(StageAccount, "operation-3", "key-3", OutcomeSucceeded, at.Add(3*time.Minute), Reference{Kind: "account", ID: "account-1"}))
	if err != nil || flow.Stage != StageProduct || flow.State != FlowPending {
		t.Fatalf("unknown recovery failed: flow=%+v err=%v", flow, err)
	}
	if _, _, err = Apply(flow, flowCommand(StageProduct, "operation-4", "key-3", OutcomeSucceeded, at.Add(4*time.Minute))); !errors.Is(err, ErrDuplicateConflict) {
		t.Fatalf("idempotency conflict error=%v, want %v", err, ErrDuplicateConflict)
	}
}

func TestFlowRejectionIsBlockedAndReferencesRemainCanonical(t *testing.T) {
	at := time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)
	flow, err := New("flow-3", "org-1", "workspace-1", "account-1", at)
	if err != nil {
		t.Fatal(err)
	}
	command := flowCommand(StageAccount, "operation-1", "key-1", OutcomeRejected, at.Add(time.Minute), Reference{Kind: "account", ID: "account-1"})
	command.ReasonCode = "missing_scope"
	flow, _, err = Apply(flow, command)
	if err != nil || flow.State != FlowBlocked || flow.Stage != StageAccount || flow.LastReasonCode != "missing_scope" || len(flow.References) != 1 {
		t.Fatalf("rejected flow=%+v err=%v", flow, err)
	}
	if _, _, err = Apply(flow, flowCommand(StageAccount, "operation-2", "key-2", OutcomeSucceeded, at.Add(2*time.Minute))); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("blocked flow accepted a command: %v", err)
	}
}

func TestFlowRejectsCommandBeforeCreationTime(t *testing.T) {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	flow, err := New("flow-4", "org-1", "workspace-1", "account-1", at)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Apply(flow, flowCommand(StageAccount, "operation-1", "key-1", OutcomeSucceeded, at.Add(-time.Second)))
	if !errors.Is(err, ErrInvalidFlow) {
		t.Fatalf("command before flow creation was accepted: %v", err)
	}
}

func TestFlowRejectsSameIdempotencyKeyWithChangedPayload(t *testing.T) {
	at := time.Date(2026, 9, 1, 13, 0, 0, 0, time.UTC)
	flow, err := New("flow-5", "org-1", "workspace-1", "account-1", at)
	if err != nil {
		t.Fatal(err)
	}
	command := flowCommand(StageAccount, "operation-1", "key-1", OutcomeSucceeded, at.Add(time.Minute), Reference{Kind: "account", ID: "account-1"})
	flow, duplicate, err := Apply(flow, command)
	if err != nil || duplicate {
		t.Fatalf("initial command: duplicate=%v err=%v", duplicate, err)
	}
	command.Outcome = OutcomeRejected
	if _, _, err = Apply(flow, command); !errors.Is(err, ErrDuplicateConflict) {
		t.Fatalf("changed idempotent payload error=%v, want %v", err, ErrDuplicateConflict)
	}
}
