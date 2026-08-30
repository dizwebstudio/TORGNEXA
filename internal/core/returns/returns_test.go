package returns

import (
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

func testUTC() time.Time { return time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC) }

func TestCancellationLifecycleRejectsBackwardTransition(t *testing.T) {
	if err := ValidateCancellationTransition(CancellationRequested, CancellationExecuting); err != nil {
		t.Fatalf("requested -> executing: %v", err)
	}
	if err := ValidateCancellationTransition(CancellationExecuting, CancellationRequested); err != ErrInvalidState {
		t.Fatalf("backward transition error = %v", err)
	}
}

func TestReturnLifecycleAndQuantityBounds(t *testing.T) {
	if err := ValidateReturnTransition(ReturnInspecting, ReturnPartiallyAccepted); err != nil {
		t.Fatalf("inspection transition: %v", err)
	}
	if err := ValidateReturnTransition(ReturnClosed, ReturnInspecting); err != ErrInvalidState {
		t.Fatalf("closed transition error = %v", err)
	}
	order, _ := NewQuantity(10, 0, "PCS")
	requested, _ := NewQuantity(3, 0, "PCS")
	received, _ := NewQuantity(2, 0, "PCS")
	accepted, _ := NewQuantity(1, 0, "PCS")
	if err := ValidateLineAllocation(order, requested, received, accepted); err != nil {
		t.Fatalf("valid allocation: %v", err)
	}
	tooMany, _ := NewQuantity(11, 0, "PCS")
	if err := ValidateLineAllocation(order, tooMany, received, accepted); err != ErrOverAllocated {
		t.Fatalf("over allocation error = %v", err)
	}
	negativeReceived, _ := NewQuantity(-1, 0, "PCS")
	if err := ValidateLineAllocation(order, requested, negativeReceived, accepted); err != ErrInvalidRecord {
		t.Fatalf("negative received quantity error = %v", err)
	}
}

func TestRefundAllocationRequiresExactPositiveMoney(t *testing.T) {
	currency, _ := domain.NewCurrency("RUB")
	zero, _ := domain.NewMoney(0, currency)
	id := RefundAllocationID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101")
	returnID := ReturnID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102")
	allocation := RefundAllocation{ID: id, OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", PaymentID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003", RefundID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0004", ReturnID: returnID, Component: RefundComponentLine, Amount: zero, Currency: currency, IdempotencyKey: "refund-allocation-1", Version: 1, CreatedAt: testUTC()}
	if err := allocation.Validate(); err != ErrInvalidRecord {
		t.Fatalf("zero allocation error = %v", err)
	}
}
