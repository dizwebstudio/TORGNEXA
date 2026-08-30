package workflowengine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/workflow"
)

type fakeStore struct {
	run      workflow.Run
	version  workflow.WorkflowVersion
	steps    map[string]workflow.StepRun
	evidence int
	released workflow.RunStatus
}

func (s *fakeStore) GetRun(context.Context, workflow.Scope, string) (workflow.Run, error) {
	return s.run, nil
}
func (s *fakeStore) Version(context.Context, workflow.Scope, string, int64) (workflow.WorkflowVersion, error) {
	return s.version, nil
}
func (s *fakeStore) UpdateRun(_ context.Context, _ workflow.Scope, v workflow.Run, _ int64) (workflow.Run, error) {
	s.run = v
	s.run.Version++
	return s.run, nil
}
func (s *fakeStore) Step(_ context.Context, _ workflow.Scope, _ string, id string) (workflow.StepRun, error) {
	v, ok := s.steps[id]
	if !ok {
		return workflow.StepRun{}, workflow.ErrNotFound
	}
	return v, nil
}
func (s *fakeStore) UpsertStep(_ context.Context, _ workflow.Scope, v workflow.StepRun, _ int64) (workflow.StepRun, error) {
	if s.steps == nil {
		s.steps = map[string]workflow.StepRun{}
	}
	v.Version++
	s.steps[v.NodeID] = v
	return v, nil
}
func (s *fakeStore) AppendEvidence(context.Context, workflow.Scope, string, string, string, int, workflow.StepStatus, string, string, time.Time) error {
	s.evidence++
	return nil
}
func (s *fakeStore) ReleaseRun(_ context.Context, _ workflow.Scope, _ string, _ string, status workflow.RunStatus, _ time.Time, _ string) error {
	s.released = status
	return nil
}

func TestExecuteUsesTypedAdapter(t *testing.T) {
	scope, _ := workflow.ParseScope("org", "ws")
	def := workflow.Definition{Name: "test", Trigger: workflow.Trigger{Kind: workflow.TriggerEvent, EventType: "commerce.catalog.product_changed.v1"}, Nodes: []workflow.Node{{ID: "action", Kind: workflow.NodeAction, Action: "sync.dry_run"}}}
	plan, _ := workflow.Compile(def)
	now := time.Now().UTC()
	store := &fakeStore{run: workflow.Run{ID: "run", OrganizationID: "org", WorkspaceID: "ws", WorkflowID: "wf", WorkflowVersion: 1, TriggerKind: workflow.TriggerEvent, IdempotencyKey: "key", InputDigest: digest("input"), Status: workflow.RunQueued, AvailableAt: now, Version: 1}, version: workflow.WorkflowVersion{ID: "ver", WorkflowID: "wf", OrganizationID: "org", WorkspaceID: "ws", Version: 1, Definition: def, PlanDigest: plan.Digest, CreatedAt: now, PublishedAt: &now}}
	called := false
	reg, _ := NewRegistry(map[string]Adapter{"sync.dry_run": AdapterFunc(func(context.Context, ActionRequest) error { called = true; return nil })})
	engine, _ := New(store, reg)
	if err := engine.Execute(context.Background(), scope, "run", "lease"); err != nil {
		t.Fatal(err)
	}
	if !called || store.evidence != 1 || store.released != workflow.RunCompleted {
		t.Fatalf("called=%v evidence=%d status=%s", called, store.evidence, store.released)
	}
}
func TestMissingAdapterFailsClosed(t *testing.T) {
	scope, _ := workflow.ParseScope("org", "ws")
	def := workflow.Definition{Name: "test", Trigger: workflow.Trigger{Kind: workflow.TriggerEvent, EventType: "commerce.catalog.product_changed.v1"}, Nodes: []workflow.Node{{ID: "action", Kind: workflow.NodeAction, Action: "sync.dry_run"}}}
	plan, _ := workflow.Compile(def)
	now := time.Now().UTC()
	store := &fakeStore{run: workflow.Run{ID: "run", OrganizationID: "org", WorkspaceID: "ws", WorkflowID: "wf", WorkflowVersion: 1, TriggerKind: workflow.TriggerEvent, IdempotencyKey: "key", InputDigest: digest("input"), Status: workflow.RunQueued, AvailableAt: now, Version: 1}, version: workflow.WorkflowVersion{ID: "ver", WorkflowID: "wf", OrganizationID: "org", WorkspaceID: "ws", Version: 1, Definition: def, PlanDigest: plan.Digest, CreatedAt: now, PublishedAt: &now}}
	reg, _ := NewRegistry(map[string]Adapter{})
	engine, _ := New(store, reg)
	if err := engine.Execute(context.Background(), scope, "run", "lease"); !errors.Is(err, ErrAdapterUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if store.released != workflow.RunFailed {
		t.Fatalf("status=%s", store.released)
	}
}

func TestFalseConditionSkipsDownstreamAction(t *testing.T) {
	scope, _ := workflow.ParseScope("org", "ws")
	def := workflow.Definition{
		Name:    "condition",
		Trigger: workflow.Trigger{Kind: workflow.TriggerEvent, EventType: "commerce.catalog.product_changed.v1"},
		Nodes: []workflow.Node{
			{ID: "condition", Kind: workflow.NodeCondition, Config: json.RawMessage(`{"result":false}`)},
			{ID: "action", Kind: workflow.NodeAction, Action: "sync.dry_run"},
		},
		Edges: []workflow.Edge{{From: "condition", To: "action", Condition: "always"}},
	}
	plan, _ := workflow.Compile(def)
	now := time.Now().UTC()
	store := &fakeStore{run: workflow.Run{ID: "run", OrganizationID: "org", WorkspaceID: "ws", WorkflowID: "wf", WorkflowVersion: 1, TriggerKind: workflow.TriggerEvent, IdempotencyKey: "key", InputDigest: digest("input"), Status: workflow.RunQueued, AvailableAt: now, Version: 1}, version: workflow.WorkflowVersion{ID: "ver", WorkflowID: "wf", OrganizationID: "org", WorkspaceID: "ws", Version: 1, Definition: def, PlanDigest: plan.Digest, CreatedAt: now, PublishedAt: &now}}
	called := false
	reg, _ := NewRegistry(map[string]Adapter{"sync.dry_run": AdapterFunc(func(context.Context, ActionRequest) error { called = true; return nil })})
	engine, _ := New(store, reg)
	if err := engine.Execute(context.Background(), scope, "run", "lease"); err != nil {
		t.Fatal(err)
	}
	if called || store.steps["action"].Status != workflow.StepSkipped || store.released != workflow.RunCompleted {
		t.Fatalf("called=%v action_status=%s run_status=%s", called, store.steps["action"].Status, store.released)
	}
}

func TestRetryBudgetEndsInTerminalFailure(t *testing.T) {
	scope, _ := workflow.ParseScope("org", "ws")
	def := workflow.Definition{Name: "retry", Trigger: workflow.Trigger{Kind: workflow.TriggerEvent, EventType: "commerce.catalog.product_changed.v1"}, Nodes: []workflow.Node{{ID: "action", Kind: workflow.NodeAction, Action: "sync.dry_run"}}}
	plan, _ := workflow.Compile(def)
	now := time.Now().UTC()
	store := &fakeStore{
		run:     workflow.Run{ID: "run", OrganizationID: "org", WorkspaceID: "ws", WorkflowID: "wf", WorkflowVersion: 1, TriggerKind: workflow.TriggerEvent, IdempotencyKey: "key", InputDigest: digest("input"), Status: workflow.RunWaitingRetry, AvailableAt: now, Version: 1},
		version: workflow.WorkflowVersion{ID: "ver", WorkflowID: "wf", OrganizationID: "org", WorkspaceID: "ws", Version: 1, Definition: def, PlanDigest: plan.Digest, CreatedAt: now, PublishedAt: &now},
		steps:   map[string]workflow.StepRun{"action": {ID: "step", RunID: "run", OrganizationID: "org", WorkspaceID: "ws", NodeID: "action", Status: workflow.StepWaitingRetry, AttemptCount: workflow.MaxStepAttempts - 1, Version: 1}},
	}
	reg, _ := NewRegistry(map[string]Adapter{"sync.dry_run": AdapterFunc(func(context.Context, ActionRequest) error { return ErrRetryable })})
	engine, _ := New(store, reg)
	if err := engine.Execute(context.Background(), scope, "run", "lease"); !errors.Is(err, ErrRetryable) {
		t.Fatalf("err=%v", err)
	}
	if store.steps["action"].Status != workflow.StepFailed || store.steps["action"].ErrorCode != "retry_exhausted" || store.released != workflow.RunFailed {
		t.Fatalf("step=%s code=%s run=%s", store.steps["action"].Status, store.steps["action"].ErrorCode, store.released)
	}
}
