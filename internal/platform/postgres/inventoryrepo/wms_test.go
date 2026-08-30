package inventoryrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/inventory"
)

func TestWMSTaskCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, time.August, 30, 10, 20, 30, 123456789, time.UTC)
	id := "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0670"

	decoded, err := decodeWMSCursor(encodeWMSCursor(at, id))
	if err != nil {
		t.Fatal(err)
	}
	if decoded == nil || !decoded.At.Equal(at) || decoded.ID != id || decoded.At.Location() != time.UTC {
		t.Fatalf("unexpected cursor: %#v", decoded)
	}
}

func TestWMSTaskCursorRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{"cursor", "v1.!", "v1." + "MTA6MDA6MDB8bad", encodeWMSCursor(time.Now().UTC(), "not-an-id")} {
		if _, err := decodeWMSCursor(value); err == nil {
			t.Fatalf("cursor %q was accepted", value)
		}
	}
}

func TestWMSFiltersAndReasonCodes(t *testing.T) {
	if !validWMSFilter("in_progress", "pick", "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0670") {
		t.Fatal("valid WMS filter was rejected")
	}
	if validWMSFilter("running", "pick", "") || validWMSFilter("pending", "unknown", "") {
		t.Fatal("invalid WMS filter was accepted")
	}
	if !validReasonValue("scan_mismatch") || validReasonValue("ScanMismatch") || validReasonValue("_invalid") || validReasonValue("scan-mismatch") {
		t.Fatal("reason-code validation is not fail-closed")
	}
}

func TestWMSStandaloneContextAndBatchBounds(t *testing.T) {
	quantity, err := inventory.NewQuantity(mustTestDecimal(t, "2.5"), inventory.UnitCode("PCS"))
	if err != nil {
		t.Fatal(err)
	}
	valid := CreateWMSTask{ID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0670", IdempotencyKey: "standalone-1", TaskType: "put_away", WarehouseID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0671", SKU: "SKU-1", SourceLocationCode: "A-01", TargetLocationCode: "B-01", ExpectedQuantity: quantity}
	if !validWMSStandaloneContext(valid) {
		t.Fatal("valid put-away context was rejected")
	}
	valid.TargetLocationCode = ""
	if validWMSStandaloneContext(valid) {
		t.Fatal("put-away without target location was accepted")
	}
	if validWMSStandaloneContext(CreateWMSTask{TaskType: "cycle_count", SourceLocationCode: "A\n01"}) {
		t.Fatal("control character in location was accepted")
	}
	ids := []string{"018f0e8b-8a58-7f42-8c2d-5c2f9b1a0670"}
	if !validWMSBatchTaskIDs(ids) || validWMSBatchTaskIDs([]string{ids[0], ids[0]}) {
		t.Fatal("batch task bounds are not fail-closed")
	}
}

func mustTestDecimal(t *testing.T, value string) inventory.Decimal {
	t.Helper()
	decimal, err := inventory.ParseDecimal(value)
	if err != nil {
		t.Fatal(err)
	}
	return decimal
}

func TestWMSDerivedIdentitiesAreStableAndSortable(t *testing.T) {
	first := wmsDerivedUUID("batch-1", "item-1", "task")
	if first != wmsDerivedUUID("batch-1", "item-1", "task") {
		t.Fatal("derived task ID is not deterministic")
	}
	if first == wmsDerivedUUID("batch-1", "item-2", "task") || !validSortableIDValue(first) {
		t.Fatalf("derived task ID is not isolated/sortable: %q", first)
	}
	if wmsDerivedKey("wms_task", "batch-1", "item-1") == wmsDerivedKey("wms_task", "batch-1", "item-2") {
		t.Fatal("derived idempotency keys collide")
	}
}

func TestWMSRepositoryKeepsAuditOutboxAndBarcodeDigestBoundaries(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	body, err := os.ReadFile(filepath.Join(filepath.Dir(source), "wms.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{
		"replayWMSTaskEvent",
		"current.Version != expectedVersion",
		"appendWMSTaskAudit",
		"enqueueWMSTaskEvent",
		"sha256.Sum256",
		"WMSCreateOrderPickTasks",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("WMS repository missing %q", required)
		}
	}
}
