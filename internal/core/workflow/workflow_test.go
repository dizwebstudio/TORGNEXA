package workflow

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func validDefinition() Definition {
	return Definition{
		Name:    "Уведомить об изменении",
		Trigger: Trigger{Kind: TriggerEvent, EventType: "commerce.catalog.product_changed.v1"},
		Nodes: []Node{
			{ID: "condition", Kind: NodeCondition, Config: json.RawMessage(`{"result":true}`)},
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

func TestDefinitionRejectsUnknownActionConfig(t *testing.T) {
	definition := validDefinition()
	definition.Nodes[1].Config = json.RawMessage(`{"unsupported":"value"}`)
	if err := definition.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown action config error = %v", err)
	}
}

func TestRunRequestRejectsNonDigestInput(t *testing.T) {
	req := RunRequest{ID: "run_1", WorkflowID: "wf_1", WorkflowVersion: 1, TriggerKind: TriggerEvent, IdempotencyKey: "request-1", InputDigest: "not-a-digest"}
	if err := req.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid run request error = %v", err)
	}
}

func TestDefinitionRejectsOversizedDocument(t *testing.T) {
	definition := validDefinition()
	definition.Description = strings.Repeat("x", MaxDescriptionLength)
	definition.Nodes[1].Config = json.RawMessage(`{"title":"` + strings.Repeat("x", 4000) + `"}`)
	definition.Nodes = append(definition.Nodes,
		Node{ID: "notify2", Kind: NodeAction, Action: "notification.create", Config: json.RawMessage(`{"title":"` + strings.Repeat("y", 4000) + `"}`)},
		Node{ID: "notify3", Kind: NodeAction, Action: "notification.create", Config: json.RawMessage(`{"title":"` + strings.Repeat("z", 4000) + `"}`)},
		Node{ID: "notify4", Kind: NodeAction, Action: "notification.create", Config: json.RawMessage(`{"title":"` + strings.Repeat("q", 4000) + `"}`)},
	)
	definition.Edges = append(definition.Edges, Edge{From: "notify", To: "notify2", Condition: "always"}, Edge{From: "notify2", To: "notify3", Condition: "always"}, Edge{From: "notify3", To: "notify4", Condition: "always"})
	if err := definition.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized definition error = %v", err)
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
