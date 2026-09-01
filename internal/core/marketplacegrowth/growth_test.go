package marketplacegrowth

import (
	"strings"
	"testing"
	"time"
)

func demoRequest() PreviewRequest {
	return PreviewRequest{
		Operation: OperationPromotionApply, ChannelID: "demo", AccountID: "account-1", TargetID: "promo-1", Currency: "RUB",
		FloorPriceMinor: 9000, MinimumMarginBPS: 1000, Items: []Candidate{{
			SKU: "SKU-1", Currency: "RUB", CurrentPriceMinor: 20000, ProposedPriceMinor: 20000, UnitCostMinor: 9000,
			CommissionBPS: 1500, LogisticsMinor: 1000, AdvertisingMinor: 500, DiscountBPS: 1000, Stock: 4,
			FactsFresh: true, Eligible: true,
		}},
	}
}

func TestBuildPreviewCalculatesIntegerEconomicsAndApproval(t *testing.T) {
	preview, err := BuildPreview("preview-1", demoRequest(), 1, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if preview.State != PreviewApprovalRequired || preview.EligibleCount != 1 || preview.BlockedCount != 0 {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	row := preview.Rows[0]
	if row.EffectivePriceMinor != 18000 || row.DiscountMinor != 2000 || row.CommissionMinor != 2700 || row.ContributionMinor != 4800 || row.MarginBPS != 2666 {
		t.Fatalf("unexpected integer economics: %#v", row)
	}
}

func TestBuildPreviewBlocksStaleFactsAndFloor(t *testing.T) {
	request := demoRequest()
	request.Items[0].FactsFresh = false
	request.Items[0].ProposedPriceMinor = 10000
	request.FloorPriceMinor = 9500
	preview, err := BuildPreview("preview-2", request, 1, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if preview.EligibleCount != 0 || preview.State != PreviewBlocked {
		t.Fatalf("stale/floor row should be blocked: %#v", preview)
	}
	reasons := strings.Join(preview.Rows[0].ReasonCodes, ",")
	if !strings.Contains(reasons, "stale_or_missing_facts") || !strings.Contains(reasons, "minimum_margin") {
		t.Fatalf("missing guard reasons: %s", reasons)
	}
}

func TestPreviewSortsSKUsAndRejectsDuplicateOrOversizedInput(t *testing.T) {
	request := demoRequest()
	request.Items = append(request.Items, Candidate{SKU: "SKU-0", Currency: "RUB", CurrentPriceMinor: 20000, UnitCostMinor: 9000, Stock: 1, FactsFresh: true, Eligible: true})
	preview, err := BuildPreview("preview-3", request, 1, time.Now().UTC())
	if err != nil || preview.Rows[0].SKU != "SKU-0" {
		t.Fatalf("expected canonical SKU ordering, preview=%#v err=%v", preview, err)
	}
	request.Items = append(request.Items, request.Items[0])
	if _, err := BuildPreview("preview-4", request, 1, time.Now().UTC()); err == nil {
		t.Fatal("duplicate SKU accepted")
	}
}

func TestOperationAndReconciliationPreserveUnknownRemoteOutcome(t *testing.T) {
	preview, err := BuildPreview("preview-5", demoRequest(), 1, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	operation, err := NewOperation("operation-1", "retry-1", "approval-1", preview, time.Now().UTC())
	if err != nil || operation.State != StateQualificationRequired {
		t.Fatalf("unexpected operation: %#v err=%v", operation, err)
	}
	drifts, err := Reconcile(operation, Observation{OperationID: operation.ID, State: StateUnknown, InputDigest: operation.InputDigest, AppliedRows: 0, ObservedAt: time.Now().UTC()})
	if err != nil || len(drifts) != 1 || drifts[0].Kind != "state_mismatch" {
		t.Fatalf("unexpected unknown reconciliation: %#v err=%v", drifts, err)
	}
}
