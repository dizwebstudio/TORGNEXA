package incidents

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"testing"
	"time"
)

type ex struct {
	failValidate bool
	rolls        int
}

func (e *ex) Action(context.Context, tenancy.Scope, string) (string, error) {
	return "action-evidence", nil
}
func (e *ex) Validate(context.Context, tenancy.Scope, string) (string, error) {
	if e.failValidate {
		return "", errors.New("bad")
	}
	return "validation-evidence", nil
}
func (e *ex) Rollback(context.Context, tenancy.Scope, string) (string, error) {
	e.rolls++
	return "rollback-evidence", nil
}
func scope(t *testing.T) tenancy.Scope {
	s, err := tenancy.ParseScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestRunbookRequiresSafeActionValidationRollbackAndEvidence(t *testing.T) {
	r := Runbook{ID: "db", Title: "DB", TriggerCodes: []string{"db_unavailable"}, Steps: []Step{{ID: "1", SafeAction: "promote-reviewed-replica", Validation: "health-and-write-probe", Rollback: "restore-routing", Evidence: "probe+timeline"}}}
	if r.Validate() != nil {
		t.Fatal("valid rejected")
	}
	r.Steps[0].Rollback = ""
	if r.Validate() == nil {
		t.Fatal("missing rollback accepted")
	}
}
func TestExecutionCollectsEvidenceAndRollsBackOnFailedValidation(t *testing.T) {
	now := time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC)
	st := NewStore()
	x := &ex{failValidate: true}
	e := NewEngine(x, st, func() time.Time { return now })
	inc := Incident{ID: "inc_1", DedupeKey: "db/primary", RunbookID: "db", Severity: SeverityP1, State: StateOpen, OpenedAt: now, UpdatedAt: now, Occurrences: 1}
	rb := Runbook{ID: "db", Title: "DB", TriggerCodes: []string{"db_unavailable"}, Steps: []Step{{ID: "1", SafeAction: "a", Validation: "v", Rollback: "r", Evidence: "e"}}}
	if err := e.Execute(context.Background(), scope(t), inc, rb); err == nil {
		t.Fatal("expected failure")
	}
	if x.rolls != 1 {
		t.Fatalf("rolls=%d", x.rolls)
	}
	if len(st.Evidence(scope(t))) < 2 {
		t.Fatalf("evidence=%+v", st.Evidence(scope(t)))
	}
}
func TestOpenDeduplicatesActiveIncident(t *testing.T) {
	now := time.Now().UTC()
	st := NewStore()
	i := Incident{ID: "inc_1", DedupeKey: "k", RunbookID: "rb", Severity: SeverityP2, State: StateOpen, OpenedAt: now, UpdatedAt: now, Occurrences: 1}
	a, _ := st.Open(scope(t), i)
	i.ID = "inc_2"
	i.UpdatedAt = now.Add(time.Minute)
	b, _ := st.Open(scope(t), i)
	if a.ID != b.ID || b.Occurrences != 2 {
		t.Fatalf("%+v", b)
	}
}
