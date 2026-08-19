package customerinbox

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"strings"
	"testing"
	"time"
)

type redactor struct{}

func (redactor) Redact(_ context.Context, _ tenancy.Scope, s string) (string, error) {
	return strings.ReplaceAll(s, "+79991234567", "[phone]"), nil
}

type sender struct{ n int }

func (s *sender) Reply(context.Context, tenancy.Scope, Conversation, string, string) (string, error) {
	s.n++
	return "remote-reply", nil
}
func TestRemoteThreadDedupPIIAndAIDraftOnly(t *testing.T) {
	sc, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	send := &sender{}
	svc := NewService(redactor{}, send)
	now := time.Now().UTC()
	c := Conversation{"c1", "social-source", "acct", "thread", "", "", now.Add(time.Hour), 1, now}
	a, e := svc.UpsertConversation(sc, c)
	if e != nil {
		t.Fatal(e)
	}
	c.ID = "c2"
	b, _ := svc.UpsertConversation(sc, c)
	if a.ID != b.ID {
		t.Fatal("remote thread duplicated")
	}
	m, e := svc.StoreMessage(context.Background(), sc, Message{"m", "c1", "rm", "in", "call +79991234567", now})
	if e != nil || strings.Contains(m.Body, "7999") {
		t.Fatalf("m=%+v e=%v", m, e)
	}
	if _, e := svc.Reply(context.Background(), sc, "c1", "draft", "ai", "i1"); !errors.Is(e, ErrAIDraftOnly) {
		t.Fatal(e)
	}
	if _, e := svc.Reply(context.Background(), sc, "c1", "send", "human", "i2"); e != nil || send.n != 1 {
		t.Fatal(e)
	}
}

func TestCaseAssignmentSLAAndTenantIsolation(t *testing.T) {
	sc, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	other, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAX", "01ARZ3NDEKTSV4RRFFQ69G5FAY")
	svc := NewService(redactor{}, &sender{})
	now := time.Now().UTC()
	if _, err := svc.UpsertConversation(sc, Conversation{ID: "case-conv", SourceSystem: "social-source", AccountID: "acct", RemoteThreadID: "case-thread", Version: 1, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	opened, err := svc.OpenCase(sc, Case{ID: "case-1", ConversationID: "case-conv", State: CaseOpen, SLADeadline: now.Add(time.Hour), Version: 1, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if SLABreached(opened, now.Add(30*time.Minute)) || !SLABreached(opened, now.Add(2*time.Hour)) {
		t.Fatal("unexpected SLA evaluation")
	}
	assigned, err := svc.Assign(sc, Assignment{CaseID: opened.ID, AssigneeID: "agent-1", At: now.Add(time.Minute)})
	if err != nil || assigned.AssigneeID != "agent-1" || assigned.Version != 2 {
		t.Fatalf("assigned=%+v err=%v", assigned, err)
	}
	if _, err := svc.GetCase(other, opened.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant case lookup error = %v", err)
	}
}
