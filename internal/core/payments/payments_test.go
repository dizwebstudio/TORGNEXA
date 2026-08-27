package payments

import (
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

const (
	tOrg     = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	tWS      = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
	tPayment = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0301"
	tRefund  = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0302"
)

func mustMoney(t *testing.T, minor int64, currency string) domain.Money {
	t.Helper()
	value, err := domain.NewMoney(minor, domain.Currency(currency))
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	return value
}

func TestParseIDs(t *testing.T) {
	if _, err := ParsePaymentID(tPayment); err != nil {
		t.Fatalf("ParsePaymentID: %v", err)
	}
	if _, err := ParsePaymentID("not-an-id"); err == nil {
		t.Fatal("expected invalid payment id to fail")
	}
	if _, err := ParseRefundID(tRefund); err != nil {
		t.Fatalf("ParseRefundID: %v", err)
	}
	if _, err := ParseScope(tOrg, tWS); err != nil {
		t.Fatalf("ParseScope: %v", err)
	}
	if _, err := ParseScope("bad", tWS); err == nil {
		t.Fatal("expected invalid scope to fail")
	}
}

func TestPaymentTransitions(t *testing.T) {
	cases := []struct {
		from, to Status
		ok       bool
	}{
		{StatusPending, StatusCreated, true},
		{StatusPending, StatusFailed, true},
		{StatusPending, StatusCanceled, true},
		{StatusPending, StatusSucceeded, false},
		{StatusCreated, StatusSucceeded, true},
		{StatusCreated, StatusFailed, true},
		{StatusSucceeded, StatusRefunded, true},
		{StatusSucceeded, StatusPartiallyRefunded, true},
		{StatusPartiallyRefunded, StatusRefunded, true},
		{StatusSucceeded, StatusCreated, false},
		{StatusFailed, StatusSucceeded, false},
		{StatusSucceeded, StatusSucceeded, false},
	}
	for _, tc := range cases {
		err := ValidatePaymentTransition(tc.from, tc.to)
		if tc.ok && err != nil {
			t.Errorf("%s->%s: expected ok, got %v", tc.from, tc.to, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s->%s: expected error, got nil", tc.from, tc.to)
		}
	}
}

func TestPaymentValidate(t *testing.T) {
	now := time.Now().UTC()
	expires := now.Add(15 * time.Minute)
	succeededAt := now.Add(2 * time.Minute)
	amount := mustMoney(t, 15000, "RUB")

	base := Payment{
		ID: PaymentID(tPayment), OrganizationID: tOrg, WorkspaceID: tWS,
		ConnectorAccountID: "yookassa-main", ExternalID: tPayment,
		Purpose: "Order #42", Amount: amount, Status: StatusPending,
		Version: 1, CreatedAt: now, UpdatedAt: now, ExpiresAt: expires,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("pending payment should validate: %v", err)
	}

	withRemoteButPending := base
	withRemoteButPending.RemoteID = "yk_123"
	if withRemoteButPending.Validate() == nil {
		t.Fatal("pending payment must not carry a remote id")
	}

	created := base
	created.Status = StatusCreated
	if created.Validate() == nil {
		t.Fatal("created payment without remote id must fail")
	}
	created.RemoteID = "yk_123"
	if err := created.Validate(); err != nil {
		t.Fatalf("created payment with remote id should validate: %v", err)
	}

	succeeded := created
	succeeded.Status = StatusSucceeded
	if succeeded.Validate() == nil {
		t.Fatal("succeeded payment without succeeded_at must fail")
	}
	succeeded.SucceededAt = &succeededAt
	if err := succeeded.Validate(); err != nil {
		t.Fatalf("succeeded payment should validate: %v", err)
	}

	failed := created
	failed.Status = StatusFailed
	if failed.Validate() == nil {
		t.Fatal("failed payment without reason code must fail")
	}
	failed.ReasonCode = "provider_declined"
	if err := failed.Validate(); err != nil {
		t.Fatalf("failed payment with reason should validate: %v", err)
	}

	badReason := created
	badReason.ReasonCode = "provider_declined"
	if badReason.Validate() == nil {
		t.Fatal("non-failed payment must not carry a reason code")
	}
}

func TestCreatePaymentValidate(t *testing.T) {
	now := time.Now().UTC()
	amount := mustMoney(t, 500, "RUB")
	valid := CreatePayment{ID: PaymentID(tPayment), ConnectorAccountID: "yookassa-main", ExternalID: tPayment, Amount: amount, ExpiresAt: now.Add(time.Hour)}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid command: %v", err)
	}
	expired := valid
	expired.ExpiresAt = now.Add(-time.Minute)
	if expired.Validate() == nil {
		t.Fatal("expected past expiry to fail")
	}
	zeroAmount := valid
	zeroAmount.Amount = domain.Money{}
	if zeroAmount.Validate() == nil {
		t.Fatal("expected zero-value amount to fail")
	}
}

func TestChangePaymentStatusValidate(t *testing.T) {
	now := time.Now().UTC()
	toCreated := ChangePaymentStatus{ID: PaymentID(tPayment), ExpectedVersion: 1, Status: StatusCreated, RemoteID: "yk_1"}
	if err := toCreated.Validate(); err != nil {
		t.Fatalf("expected valid: %v", err)
	}
	toSucceededMissingTime := ChangePaymentStatus{ID: PaymentID(tPayment), ExpectedVersion: 2, Status: StatusSucceeded}
	if toSucceededMissingTime.Validate() == nil {
		t.Fatal("succeeded transition requires succeeded_at")
	}
	toSucceeded := toSucceededMissingTime
	toSucceeded.SucceededAt = &now
	if err := toSucceeded.Validate(); err != nil {
		t.Fatalf("expected valid: %v", err)
	}
	toFailedNoReason := ChangePaymentStatus{ID: PaymentID(tPayment), ExpectedVersion: 2, Status: StatusFailed}
	if toFailedNoReason.Validate() == nil {
		t.Fatal("failed transition requires reason code")
	}
}

func TestRefundTransitionsAndValidate(t *testing.T) {
	cases := []struct {
		from, to RefundStatus
		ok       bool
	}{
		{RefundPending, RefundAccepted, true},
		{RefundPending, RefundFailed, true},
		{RefundAccepted, RefundSucceeded, true},
		{RefundAccepted, RefundFailed, true},
		{RefundPending, RefundSucceeded, false},
		{RefundSucceeded, RefundFailed, false},
	}
	for _, tc := range cases {
		err := ValidateRefundTransition(tc.from, tc.to)
		if tc.ok && err != nil {
			t.Errorf("%s->%s: expected ok, got %v", tc.from, tc.to, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s->%s: expected error, got nil", tc.from, tc.to)
		}
	}

	now := time.Now().UTC()
	amount := mustMoney(t, 500, "RUB")
	pending := Refund{ID: RefundID(tRefund), OrganizationID: tOrg, WorkspaceID: tWS, PaymentID: PaymentID(tPayment), ExternalID: tRefund, Amount: amount, Status: RefundPending, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := pending.Validate(); err != nil {
		t.Fatalf("expected valid pending refund: %v", err)
	}
	pendingWithRemote := pending
	pendingWithRemote.RemoteRefundID = "yk_ref_1"
	if pendingWithRemote.Validate() == nil {
		t.Fatal("pending refund must not carry a remote id")
	}
	accepted := pending
	accepted.Status = RefundAccepted
	if accepted.Validate() == nil {
		t.Fatal("accepted refund requires remote id")
	}
}

func TestWebhookEvidenceValidate(t *testing.T) {
	now := time.Now().UTC()
	digest := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"[:64]
	valid := WebhookEvidence{DeliveryID: "delivery-1", ConnectorAccountID: "yookassa-main", RemotePaymentID: "yk_1", EventType: "payment.succeeded", BodyDigest: digest, VerifiedAt: now}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid: %v", err)
	}
	badDigest := valid
	badDigest.BodyDigest = "too-short"
	if badDigest.Validate() == nil {
		t.Fatal("expected invalid digest to fail")
	}
}

func TestMutationValidate(t *testing.T) {
	m := Mutation{EventID: "evt-1", AuditID: tPayment, ActorID: "user:1", Source: "api.payments", CorrelationID: "corr-1", OccurredAt: time.Now().UTC()}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid: %v", err)
	}
	notUTC := m
	notUTC.OccurredAt = time.Now()
	if notUTC.OccurredAt.Location() != time.UTC && notUTC.Validate() == nil {
		t.Fatal("expected non-UTC occurred_at to fail")
	}
}
