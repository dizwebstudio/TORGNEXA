package marketplaceoperations

import (
	"testing"
	"time"
)

func TestFindingRejectsNonOpenPersistedState(t *testing.T) {
	finding := Finding{
		ID: "finding-1", OrganizationID: "org-1", WorkspaceID: "workspace-1", FlowID: "flow-1", AccountID: "account-1",
		Stage: StageInventory, Kind: FindingPriceStockMismatch, EntityKind: "inventory", EntityID: "inventory-1", Severity: FindingWarn,
		Status: FindingResolved, ReasonCode: "stock_mismatch", DetectedAt: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC),
	}
	if err := finding.Validate(); err == nil {
		t.Fatal("resolved finding was accepted as a new append-only finding")
	}
}

func TestFindingActionValidation(t *testing.T) {
	action := FindingAction{ID: "action-1", FindingID: "finding-1", Action: FindingActionReconcile, IdempotencyKey: "key-1", ActorID: "operator-1", OccurredAt: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)}
	if err := action.Validate(); err != nil {
		t.Fatalf("valid finding action rejected: %v", err)
	}
}
