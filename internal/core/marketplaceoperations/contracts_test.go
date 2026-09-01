package marketplaceoperations

import (
	"errors"
	"testing"
	"time"
)

func TestSuccessfulStageRequiresCanonicalReference(t *testing.T) {
	command := flowCommand(StageShipment, "operation-1", "key-1", OutcomeSucceeded, time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	if !errors.Is(ValidateCommandReferences(command), ErrMissingReference) {
		t.Fatalf("missing shipment reference error=%v", ValidateCommandReferences(command))
	}
	command.References = []Reference{{Kind: "shipment", ID: "shipment-1"}}
	if err := ValidateCommandReferences(command); err != nil {
		t.Fatalf("valid shipment reference rejected: %v", err)
	}
}

func TestRejectedStageCanRecordFailureWithoutCanonicalReference(t *testing.T) {
	command := flowCommand(StageOrder, "operation-1", "key-1", OutcomeUnknown, time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	if err := ValidateCommandReferences(command); err != nil {
		t.Fatalf("unknown order outcome rejected without reference: %v", err)
	}
}
