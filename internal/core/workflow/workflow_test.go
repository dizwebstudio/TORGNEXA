package workflow

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func validDefinition() Definition {
	return Definition{
		Name:    "Уведомить об изменении",
		Trigger: Trigger{Kind: TriggerEvent, EventType: "commerce.catalog.product_changed.v1"},
		Nodes: []Node{
			{ID: "condition", Kind: NodeCondition, Config: json.RawMessage(`{"key":"status"}`)},
			{ID: "notify", Kind: NodeAction, Action: "notification.create", Config: json.RawMessage(`{"category":"commerce"}`)},
		},
		Edges: []Edge{{From: "condition", To: "notify", Condition: "always"}},
	}
}

func TestCompileIsDeterministic(t *testing.T) {
	plan, err := Compile(validDefinition())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plan.NodeIDs, []string{"condition", "notify"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("order = %#v", got)
	}
	again, err := Compile(validDefinition())
	if err != nil || again.Digest != plan.Digest {
		t.Fatalf("compile not deterministic: %#v / %#v", plan, again)
	}
}

func TestCompileRejectsCycle(t *testing.T) {
	definition := validDefinition()
	definition.Edges = append(definition.Edges, Edge{From: "notify", To: "condition"})
	if !errors.Is(mustCompileError(definition), ErrGraphCycle) {
		t.Fatal("cycle must be rejected")
	}
}

func TestCompileRejectsDisconnectedGraph(t *testing.T) {
	definition := validDefinition()
	definition.Nodes = append(definition.Nodes, Node{ID: "orphan", Kind: NodeAction, Action: "sync.dry_run"})
	if !errors.Is(mustCompileError(definition), ErrGraphUnreachable) {
		t.Fatal("disconnected node must be rejected")
	}
}

func TestDefinitionRejectsSecretConfig(t *testing.T) {
	definition := validDefinition()
	definition.Nodes[1].Config = json.RawMessage(`{"api_token":"not allowed"}`)
	if err := definition.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("secret-shaped config error = %v", err)
	}
}

func TestScheduleTriggerBounds(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	definition := validDefinition()
	definition.Trigger = Trigger{Kind: TriggerSchedule, IntervalMinutes: 5, Enabled: true, NextRunAt: &now}
	if err := definition.Validate(); err != nil {
		t.Fatal(err)
	}
	definition.Trigger.IntervalMinutes = MaxScheduleMinutes + 1
	if err := definition.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatal("oversized schedule must be rejected")
	}
}

func TestLifecycleTransitions(t *testing.T) {
	if err := ValidateTransition(StatusDraft, StatusPublished); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(ValidateTransition(StatusPublished, StatusDraft), ErrInvalidState) {
		t.Fatal("published -> draft must be rejected")
	}
}

func mustCompileError(definition Definition) error {
	_, err := Compile(definition)
	return err
}
