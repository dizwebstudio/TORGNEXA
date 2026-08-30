package connectors

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func TestMarkingRequestsNeverCarryRawCodes(t *testing.T) {
	request := MarkingAggregationRequest{
		MarkingOperationRequest: MarkingOperationRequest{ExternalID: "batch-1", IdempotencyKey: "idem-1", DryRun: true},
		PackageRef:              "package-1",
		ChildFingerprints:      []string{strings.Repeat("a", 64)},
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("validate aggregation: %v", err)
	}
	if strings.Contains(strings.ToLower(string(mustJSON(t, request))), "raw") {
		t.Fatal("marking request contains a raw-code field")
	}
}

func TestMarkingReceiptAllowsUnknownOutcome(t *testing.T) {
	receipt := MarkingOperationReceipt{Status: MarkingUnknown, ObservedAt: time.Now().UTC()}
	if err := receipt.Validate(); err != nil {
		t.Fatalf("unknown receipt rejected: %v", err)
	}
}

func TestMarkingCapabilitiesAreWriteSensitive(t *testing.T) {
	for _, name := range []Capability{"marking.codes.request", "marking.codes.reserve", "marking.aggregation.write", "marking.circulation.introduce", "marking.circulation.withdraw", "marking.transfer.write"} {
		definition, ok := CapabilityDefinitionFor(name)
		if !ok || definition.Direction != CapabilityWrite || !definition.ApprovalRequired || definition.Risk != CapabilityRiskWriteSensitive {
			t.Fatalf("capability %q is not a guarded write", name)
		}
	}
}
