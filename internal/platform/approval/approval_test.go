package approval

import (
	"errors"
	"testing"
	"time"
)

const (
	id1 = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	id2 = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
	id3 = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003"
	id4 = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0004"
)

func policy() Policy {
	return Policy{ID: id1, OrganizationID: id2, WorkspaceID: id3, Name: "sensitive_write", Action: "pricing.bulk_update", ResourceType: "price", MinimumRisk: RiskWriteSensitive, Version: 1, RequestTTL: 2 * time.Hour, EscalateAfter: 15 * time.Minute, SeparationOfDuties: true, Active: true, Stages: []Stage{{1, "business", 2, []string{"approval.pricing"}}, {2, "finance", 1, []string{"approval.finance"}}}}
}
func TestGateFailsClosedForSensitiveWrites(t *testing.T) {
	if GateDecision(RiskWriteSensitive, false) != DecisionDeny || GateDecision(RiskWriteSensitive, true) != DecisionRequire || GateDecision(RiskWriteSafe, false) != DecisionAllow {
		t.Fatal("unexpected gate decision")
	}
}

func TestLegacyDemoApprovalIdentifiersRemainReadable(t *testing.T) {
	p := policy()
	p.ID = "demo-approval-policy"
	if err := p.Validate(); err != nil {
		t.Fatalf("legacy demo policy should remain readable: %v", err)
	}
	now := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	r, err := NewRequest(p, "demo-approval-pending", "requester", "demo.seed", "price-set-1", "demo-seed:approval:pending", RiskWriteSensitive, now)
	if err != nil {
		t.Fatalf("legacy demo request should remain readable: %v", err)
	}
	if _, _, err := ApplyDecision(r, p, nil, Actor{ID: "approver", Scopes: []string{"approval.pricing"}}, "demo-approval-decision-approved", VoteApprove, "ok", now.Add(time.Minute)); err != nil {
		t.Fatalf("legacy demo decision should remain readable: %v", err)
	}
}

func TestFourEyesQuorumAndMultiStage(t *testing.T) {
	p := policy()
	now := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	r, err := NewRequest(p, id4, "requester", "api", "price-set-1", "corr-1", RiskWriteSensitive, now)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = ApplyDecision(r, p, nil, Actor{ID: "requester", Scopes: []string{"approval.pricing"}}, "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0005", VoteApprove, "ok", now.Add(time.Minute))
	if !errors.Is(err, ErrSeparation) {
		t.Fatalf("expected separation, got %v", err)
	}
	d1id := "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0006"
	r1, d1, err := ApplyDecision(r, p, nil, Actor{ID: "approver-a", Scopes: []string{"approval.pricing"}}, d1id, VoteApprove, "ok", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if r1.CurrentStage != 1 || r1.State != StatePending {
		t.Fatal("quorum reached too early")
	}
	r2, d2, err := ApplyDecision(r1, p, []DecisionRecord{d1}, Actor{ID: "approver-b", Scopes: []string{"approval.pricing"}}, "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0007", VoteApprove, "ok", now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if r2.CurrentStage != 2 || r2.State != StatePending {
		t.Fatalf("stage=%d state=%s", r2.CurrentStage, r2.State)
	}
	r3, _, err := ApplyDecision(r2, p, []DecisionRecord{d1, d2}, Actor{ID: "approver-c", Scopes: []string{"approval.finance"}}, "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0008", VoteApprove, "ok", now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if r3.State != StateApproved || r3.ApprovedAt == nil {
		t.Fatal("not approved")
	}
}
func TestRejectIsTerminalAndDuplicateVoteFails(t *testing.T) {
	p := policy()
	now := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	r, _ := NewRequest(p, id4, "requester", "api", "price-set-1", "corr-1", RiskWriteSensitive, now)
	r1, d1, err := ApplyDecision(r, p, nil, Actor{ID: "a", Scopes: []string{"approval.pricing"}}, "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0005", VoteApprove, "ok", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = ApplyDecision(r1, p, []DecisionRecord{d1}, Actor{ID: "a", Scopes: []string{"approval.pricing"}}, "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0006", VoteApprove, "ok", now.Add(2*time.Minute))
	if !errors.Is(err, ErrAlreadyVoted) {
		t.Fatalf("%v", err)
	}
	r2, _, err := ApplyDecision(r1, p, []DecisionRecord{d1}, Actor{ID: "b", Scopes: []string{"approval.pricing"}}, "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0007", VoteReject, "no", now.Add(2*time.Minute))
	if err != nil || r2.State != StateRejected {
		t.Fatalf("%v %s", err, r2.State)
	}
}
func TestExpiryEscalationAndExecutionLifecycle(t *testing.T) {
	p := policy()
	now := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	r, _ := NewRequest(p, id4, "requester", "api", "price-set-1", "corr-1", RiskWriteSensitive, now)
	if _, err := Escalate(r, p, now.Add(14*time.Minute)); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected early escalation reject: %v", err)
	}
	e, err := Escalate(r, p, now.Add(15*time.Minute))
	if err != nil || e.EscalationCount != 1 {
		t.Fatal(err)
	}
	x, err := Expire(e, now.Add(2*time.Hour))
	if err != nil || x.State != StateExpired {
		t.Fatal(err)
	}
	a := r
	a.State = StateApproved
	a.ApprovedAt = timePtr(now)
	ex, err := BeginExecution(a, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	done, err := CompleteExecution(ex, true, "", now.Add(2*time.Minute))
	if err != nil || done.State != StateCompleted {
		t.Fatal(err)
	}
}
func TestCommentRedactsCredentials(t *testing.T) {
	got, err := SanitizeComment("Authorization: Bearer top-secret")
	if err != nil {
		t.Fatal(err)
	}
	if got == "Authorization: Bearer top-secret" || got == "" {
		t.Fatalf("unsafe redaction: %q", got)
	}
}

func TestExecutionGrantIsBoundToExactApprovedResource(t *testing.T) {
	p := policy()
	now := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	r, _ := NewRequest(p, id4, "requester", "api", "price-set-1", "corr-1", RiskWriteSensitive, now)
	a := r
	a.State = StateApproved
	a.ApprovedAt = timePtr(now.Add(time.Minute))
	ex, err := BeginExecution(a, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	g, err := Grant(ex, "pricing.bulk_update", "price", "price-set-1")
	if err != nil || g.RequestID != ex.ID {
		t.Fatalf("grant=%#v err=%v", g, err)
	}
	if _, err := Grant(ex, "pricing.bulk_update", "price", "price-set-2"); !errors.Is(err, ErrDenied) {
		t.Fatalf("resource substitution accepted: %v", err)
	}
}
func TestApprovedRequestCannotBeginAfterExpiry(t *testing.T) {
	p := policy()
	now := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	r, _ := NewRequest(p, id4, "requester", "api", "price-set-1", "corr-1", RiskWriteSensitive, now)
	r.State = StateApproved
	r.ApprovedAt = timePtr(now.Add(time.Minute))
	if _, err := BeginExecution(r, r.ExpiresAt); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired approval executed: %v", err)
	}
}
