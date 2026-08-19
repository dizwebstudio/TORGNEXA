package reporting

import (
	"os"
	"strings"
	"testing"
)

func TestClickHouseSchemaReplayAndCurrencySafety(t *testing.T) {
	raw, err := os.ReadFile("../../../deploy/clickhouse/000001_reporting_foundation.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	required := []string{
		"ReplacingMergeTree(ingest_version)",
		"AggregatingMergeTree()",
		"uniqExactState(event_id)",
		"argMaxState(JSONExtractString(analytics_data_json, 'status')",
		"GROUP BY day, organization_id, workspace_id, currency",
		"inventory_state_mv_v1",
		"freshness_v1",
	}
	for _, token := range required {
		if !strings.Contains(sql, token) {
			t.Fatalf("schema missing %q", token)
		}
	}
	forbidden := []string{"FINAL", "OPTIMIZE TABLE", "target_currency", "convertCurrency", "async_insert=1,wait_for_async_insert=0"}
	for _, token := range forbidden {
		if strings.Contains(sql, token) {
			t.Fatalf("schema contains forbidden %q", token)
		}
	}
}

func TestAnalyticsPayloadIsMinimized(t *testing.T) {
	// The allow-list is intentionally small: freshness needs only the envelope;
	// raw payloads are admitted solely for fact schemas owned by Task 049.
	raw, err := os.ReadFile("reporting.go")
	if err != nil {
		t.Fatal(err)
	}
	code := string(raw)
	if !strings.Contains(code, `case "commerce.orders.order_changed.v1", "commerce.inventory.stock_changed.v1":`) {
		t.Fatal("analytics payload allow-list changed without review")
	}
}
