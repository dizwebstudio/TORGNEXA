package reporting

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

func TestClickHouseQueryPortBindsTenantAndDecodesReports(t *testing.T) {
	scope, err := tenancy.ParseScope(orgID, wsID)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Query().Get("param_organization_id") != orgID || r.URL.Query().Get("param_workspace_id") != wsID {
			t.Fatalf("request is not tenant-bound: %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("X-ClickHouse-User") != "reporter" || r.Header.Get("X-ClickHouse-Key") != "private" {
			t.Fatal("ClickHouse credentials were not sent through dedicated headers")
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(string(body), "sales_daily_v1"):
			_, _ = w.Write([]byte(`{"day":"2026-08-10","currency":"RUB","orders":2,"fulfilled_orders":1,"cancelled_orders":1,"gross_minor_units":1000}` + "\n"))
		case strings.Contains(string(body), "inventory_current_v1"):
			_, _ = w.Write([]byte(`{"offer_id":"offer-1","warehouse_id":"warehouse-1","quantity":"2.50","changed_at":"2026-08-10T12:00:00.000000Z","event_id":"event-1"}` + "\n"))
		case strings.Contains(string(body), "freshness_v1"):
			_, _ = w.Write([]byte(`{"event_family":"commerce.orders","last_occurred_at":"2026-08-10T12:00:00.000000Z","last_ingested_at":"2026-08-10T12:00:02.000000Z","observed_at":"2026-08-10T12:00:03.000000Z","source_lag_seconds":2,"event_count":4}` + "\n"))
		default:
			http.Error(w, "unexpected query", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	port, err := NewClickHouseQueryPort(ClickHouseConfig{Endpoint: server.URL, Username: "reporter", Password: "private", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	sales, err := port.Sales(context.Background(), scope, SalesQuery{From: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Limit: 10})
	if err != nil || len(sales) != 1 || sales[0].GrossMinorUnits != 1000 {
		t.Fatalf("sales=%+v err=%v", sales, err)
	}
	inventory, err := port.Inventory(context.Background(), scope, InventoryQuery{Limit: 10})
	if err != nil || len(inventory) != 1 || inventory[0].Quantity != "2.50" {
		t.Fatalf("inventory=%+v err=%v", inventory, err)
	}
	freshness, err := port.Freshness(context.Background(), scope)
	if err != nil || len(freshness) != 1 || freshness[0].SourceLag != 2*time.Second {
		t.Fatalf("freshness=%+v err=%v", freshness, err)
	}
}

func TestClickHouseSinkAppendsEventFactRowsAsJSONEachRow(t *testing.T) {
	rec, err := NewRecord(
		event(t, "evt-order-1", "commerce.orders.order_changed.v1", "order-1", "2026-08-10T10:00:00Z", map[string]any{"order_id": "01J00000000000000000000010", "status": "confirmed", "total": map[string]any{"minor_units": 1000, "currency": "RUB"}, "version": 1}),
		time.Date(2026, 8, 10, 10, 0, 1, 0, time.UTC), SourcePosition{Stream: "commerce.orders.events", Partition: 0, Offset: 1}, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := NewBatch([]Record{rec})
	if err != nil {
		t.Fatal(err)
	}

	var gotQuery, gotBody, gotUser, gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		gotQuery = r.URL.Query().Get("query")
		gotUser, gotKey = r.Header.Get("X-ClickHouse-User"), r.Header.Get("X-ClickHouse-Key")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
	}))
	defer server.Close()

	sink, err := NewClickHouseSink(ClickHouseConfig{Endpoint: server.URL, Username: "reporter", Password: "private", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Append(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	if gotQuery != insertEventFactStatement {
		t.Fatalf("query=%q", gotQuery)
	}
	if gotUser != "reporter" || gotKey != "private" {
		t.Fatalf("credentials not sent through dedicated headers: user=%q key=%q", gotUser, gotKey)
	}
	if !strings.Contains(gotBody, `"event_id":"evt-order-1"`) || !strings.Contains(gotBody, `"analytics_data_json":"{`) {
		t.Fatalf("body=%q", gotBody)
	}
	if strings.Contains(gotBody, `"analytics_data_json":{`) {
		t.Fatalf("analytics_data_json must be an embedded JSON string, not a nested object: body=%q", gotBody)
	}
}

func TestClickHouseSinkRejectsTamperedDedupToken(t *testing.T) {
	rec, err := NewRecord(
		event(t, "evt-order-1", "commerce.orders.order_changed.v1", "order-1", "2026-08-10T10:00:00Z", map[string]any{"order_id": "01J00000000000000000000010", "status": "confirmed", "total": map[string]any{"minor_units": 1000, "currency": "RUB"}, "version": 1}),
		time.Date(2026, 8, 10, 10, 0, 1, 0, time.UTC), SourcePosition{Stream: "commerce.orders.events", Partition: 0, Offset: 1}, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	batch := Batch{Records: []Record{rec}, DedupToken: "tampered"}
	sink, err := NewClickHouseSink(ClickHouseConfig{Endpoint: "http://127.0.0.1:1", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Append(context.Background(), batch); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestClickHouseQueryPortDoesNotExposeServerBodyOrCredentials(t *testing.T) {
	scope, _ := tenancy.ParseScope(orgID, wsID)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private-diagnostic", http.StatusInternalServerError)
	}))
	defer server.Close()
	port, _ := NewClickHouseQueryPort(ClickHouseConfig{Endpoint: server.URL, Username: "reporter", Password: "private-password", Timeout: time.Second})
	_, err := port.Freshness(context.Background(), scope)
	if err == nil || strings.Contains(err.Error(), "private-diagnostic") || strings.Contains(err.Error(), "private-password") {
		t.Fatalf("unsafe error: %v", err)
	}
}
