package marketplaceoperations

import (
	"testing"
	"time"
)

func TestFindingObservationClassifiesUrgentCauseFirst(t *testing.T) {
	observation := FindingObservation{UnknownRemoteOutcome: true, StaleData: true, StatusDrift: true}
	if got := observation.Classify(); got != FindingUnknownRemoteOutcome {
		t.Fatalf("classification=%s, want unknown_remote_outcome", got)
	}

	observation = FindingObservation{MissingMapping: true, DuplicateOrder: true}
	if got := observation.Classify(); got != FindingMissingMapping {
		t.Fatalf("classification=%s, want missing_mapping", got)
	}
}

func TestBuildFindingIsSafeAndOpen(t *testing.T) {
	finding, err := BuildFinding(FindingObservation{
		ID: "finding-1", OrganizationID: "org-1", WorkspaceID: "ws-1", FlowID: "flow-1", AccountID: "account-1",
		Stage: StageOrder, EntityKind: "order", EntityID: "remote-order-1", DuplicateOrder: true,
		Expected: "one", Observed: "two", DetectedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildFinding() error=%v", err)
	}
	if finding.Kind != FindingDuplicateOrder || finding.Status != FindingOpen || finding.Severity != FindingWarn || finding.ReasonCode != string(FindingDuplicateOrder) {
		t.Fatalf("unexpected finding: %#v", finding)
	}
	if _, err := BuildFinding(FindingObservation{ID: "finding-2", OrganizationID: "org-1", WorkspaceID: "ws-1", FlowID: "flow-1", AccountID: "account-1", Stage: StageOrder, EntityKind: "order", EntityID: "order-1", DetectedAt: time.Now().UTC()}); err == nil {
		t.Fatal("empty classification accepted")
	}
}
