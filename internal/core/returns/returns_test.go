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

func TestQuantityValidationRejectsMalformedUnits(t *testing.T) {
	if _, err := NewQuantity(1, 0, "pcs"); err != ErrInvalidRecord {
		t.Fatalf("lowercase unit error = %v", err)
	}
	if err := (Quantity{Coefficient: 1, Unit: "PCS!"}).Validate(); err != ErrInvalidRecord {
		t.Fatalf("punctuated unit error = %v", err)
	}
}

func TestInspectionRequiresQuantityForAcceptedOutcomes(t *testing.T) {
	now := testUTC()
	zero, _ := NewQuantity(0, 0, "PCS")
	inspection := InspectionResult{
		ID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0201", ReturnID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0202", ReturnItemID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0203",
		Outcome: ReturnAccepted, ConditionCode: "sealed", Quantity: zero, Disposition: DispositionRestock, OccurredAt: now,
	}
	if err := inspection.Validate(); err != ErrInvalidRecord {
		t.Fatalf("zero accepted inspection error = %v", err)
	}
	inspection.Outcome = ReturnRejected
	if err := inspection.Validate(); err != nil {
		t.Fatalf("zero rejected inspection: %v", err)
	}
}

func TestReturnLogisticsOperationRequiresRemoteResultOnlyOnSuccess(t *testing.T) {
	now := testUTC()
	base := ReturnLogisticsOperation{
		ID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0301", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002",
		ReturnID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0302", ConnectorAccountID: "pochta-account", OriginalRemoteID: "RA644000001RU", ExternalID: "return-001", MailType: "POSTAL_PARCEL",
		Status: ReturnLogisticsSucceeded, RemoteID: "57565818", IdempotencyKey: "return-logistics-001", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid successful operation: %v", err)
	}
	base.Status = ReturnLogisticsUnknown
	base.RemoteID = ""
	if err := base.Validate(); err != nil {
		t.Fatalf("valid unknown operation: %v", err)
	}
	base.Status = ReturnLogisticsSucceeded
	base.RemoteID = ""
	if err := base.Validate(); err != ErrInvalidRecord {
		t.Fatalf("success without remote id error = %v", err)
	}
}

func TestReturnLogisticsResultIsCreatedOnly(t *testing.T) {
	result := ReturnLogisticsResult{RemoteID: "57565818", Status: "created", ObservedAt: testUTC()}
	if err := result.Validate(); err != nil {
		t.Fatalf("created result: %v", err)
	}
	result.Status = "in_transit"
	if err := result.Validate(); err != ErrInvalidRecord {
		t.Fatalf("unexpected result status error = %v", err)
	}
}
