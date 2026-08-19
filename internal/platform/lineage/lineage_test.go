package lineage

import (
	"testing"
	"time"
)

const (
	orgID = "018f0000-0000-7000-8000-000000000001"
	wsID  = "018f0000-0000-7000-8000-000000000002"
)

func validRecord(t *testing.T) Record {
	t.Helper()
	at := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	id, err := DeterministicID("evt.price.1")
	if err != nil {
		t.Fatal(err)
	}
	return Record{
		ID: id, OrganizationID: orgID, WorkspaceID: wsID, Source: "api", ActorID: "user-1", Operation: "price.updated",
		Output:         Ref{System: "torgnexa", EntityType: "price", EntityID: "p1", Version: "2", Field: "amount"},
		Inputs:         []Input{{Role: "previous", Ref: Ref{System: "torgnexa", EntityType: "price", EntityID: "p1", Version: "1", Field: "amount", ObservedAt: &at}}},
		Transformation: Transformation{Kind: "domain_mutation", ID: "pricing.update", Version: "1"},
		CorrelationID:  "corr-1", AuditID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", EventID: "evt.price.1", Result: ResultApplied, OccurredAt: at,
	}
}

func TestRecordValidationAndDeterministicID(t *testing.T) {
	r := validRecord(t)
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	a, _ := DeterministicID(r.EventID)
	b, _ := DeterministicID(r.EventID)
	if a != b || len(a) != 68 {
		t.Fatalf("deterministic id mismatch: %q %q", a, b)
	}
}
func TestRecordRejectsLocalTimeAndDuplicateInput(t *testing.T) {
	r := validRecord(t)
	r.OccurredAt = r.OccurredAt.In(time.FixedZone("x", 3600))
	if r.Validate() == nil {
		t.Fatal("expected local time rejection")
	}
	r = validRecord(t)
	r.Inputs = append(r.Inputs, r.Inputs[0])
	if r.Validate() == nil {
		t.Fatal("expected duplicate input rejection")
	}
}
func TestTimelineQueryRequiresBoundedCursor(t *testing.T) {
	q := TimelineQuery{System: "torgnexa", EntityType: "price", EntityID: "p1", Limit: 100}
	if err := q.Validate(); err != nil {
		t.Fatal(err)
	}
	q.BeforeID = "x"
	if q.Validate() == nil {
		t.Fatal("cursor id without time must fail")
	}
}
