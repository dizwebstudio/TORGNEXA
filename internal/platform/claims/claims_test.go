package claims

import (
	"context"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"testing"
	"time"
)

type verifier bool

func (v verifier) Released(context.Context, tenancy.Scope, string) bool { return bool(v) }
func TestEvidenceRequiresReleasedUploadAndSLAEscalates(t *testing.T) {
	scope, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	now := time.Now().UTC()
	c := Claim{ID: "c1", Context: ContextMarketplace, State: StateOpen, Version: 1, EscalationAt: now.Add(time.Hour), UpdatedAt: now}
	if _, e := (Service{Verifier: verifier(false)}).AddEvidence(context.Background(), scope, c, Evidence{ID: "e", UploadID: "u", ObjectRef: "s3://released/e", MediaType: "image/jpeg"}, now); e == nil {
		t.Fatal("unreleased evidence accepted")
	}
	got, e := (Service{Verifier: verifier(true)}).AddEvidence(context.Background(), scope, c, Evidence{ID: "e", UploadID: "u", ObjectRef: "s3://released/e", MediaType: "image/jpeg"}, now)
	if e != nil || len(got.Evidence) != 1 {
		t.Fatal(e)
	}
	if !DueForEscalation(got, now.Add(2*time.Hour)) {
		t.Fatal("expected escalation")
	}
}

type auditRecorder struct {
	calls  int
	claim  string
	action string
}

func (a *auditRecorder) Record(_ context.Context, _ tenancy.Scope, claimID, action string, _ time.Time) error {
	a.calls++
	a.claim = claimID
	a.action = action
	return nil
}

func TestEvidenceWritesAuditEvidence(t *testing.T) {
	scope, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	now := time.Now().UTC()
	audit := &auditRecorder{}
	claim := Claim{ID: "claim-audit", Context: ContextCarrier, State: StateOpen, Version: 1, UpdatedAt: now}
	_, err := (Service{Verifier: verifier(true), Audit: audit}).AddEvidence(context.Background(), scope, claim, Evidence{ID: "ev", UploadID: "upload", ObjectRef: "s3://released/audit", MediaType: "application/pdf"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if audit.calls != 1 || audit.claim != claim.ID || audit.action != "evidence.added" {
		t.Fatalf("audit=%+v", audit)
	}
}
