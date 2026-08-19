package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

const (
	orgID = "01J00000000000000000000000"
	wsID  = "01J00000000000000000000001"
)

func instant(t *testing.T, value string) domain.UTCInstant {
	t.Helper()
	parsed, err := domain.ParseUTCInstant(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func event(t *testing.T, id, typ, entity, at string, data any) eventbus.Event {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	et, err := eventbus.ParseEventType(typ)
	if err != nil {
		t.Fatal(err)
	}
	out := eventbus.Event{ID: id, Type: et, OccurredAt: instant(t, at), OrganizationID: orgID, WorkspaceID: wsID, EntityType: "test", EntityID: entity, Source: "test", Data: raw}
	if err := out.Validate(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBatchRejectsCrossTenantAndDuplicate(t *testing.T) {
	base := event(t, "evt-1", "commerce.inventory.stock_changed.v1", "offer-1", "2026-08-10T10:00:00Z", map[string]any{"offer_id": "offer-1", "warehouse_id": "wh-1", "old_quantity": "1", "new_quantity": "2"})
	rec, _ := NewRecord(base, time.Date(2026, 8, 10, 10, 0, 1, 0, time.UTC), SourcePosition{Stream: "commerce.inventory.events", Partition: 0, Offset: 1}, "")
	if _, err := NewBatch([]Record{rec, rec}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate err=%v", err)
	}
	other := rec
	other.EventID = "evt-2"
	other.WorkspaceID = "01J00000000000000000000002"
	if _, err := NewBatch([]Record{rec, other}); !errors.Is(err, ErrCrossTenantBatch) {
		t.Fatalf("cross tenant err=%v", err)
	}
}

func TestIngestReplayIsSemanticallyIdempotentAndPreservesCurrency(t *testing.T) {
	projection := NewMemoryProjection()
	now := func() time.Time { return time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC) }
	ingestor, _ := NewIngestor(projection, now)
	first := event(t, "evt-order-1", "commerce.orders.order_changed.v1", "order-1", "2026-08-10T10:00:00Z", map[string]any{
		"order_id": "01J00000000000000000000010", "number": "N1", "status": "confirmed", "total": map[string]any{"minor_units": 1000, "currency": "RUB"}, "item_count": 1, "items": []any{map[string]any{"item_id": "01J00000000000000000000011", "offer_id": "01J00000000000000000000012", "sku": "SKU", "quantity": map[string]any{"value": "1", "unit": "PCS"}, "unit_price": map[string]any{"minor_units": 1000, "currency": "RUB"}, "line_total": map[string]any{"minor_units": 1000, "currency": "RUB"}, "tax": map[string]any{"jurisdiction": "RU", "category": "standard", "rate_fraction": "0.2", "price_includes_tax": true}}}, "version": 1, "change": "created"})
	second := event(t, "evt-order-2", "commerce.orders.order_changed.v1", "order-1", "2026-08-10T11:00:00Z", map[string]any{
		"order_id": "01J00000000000000000000010", "number": "N1", "status": "fulfilled", "old_status": "confirmed", "total": map[string]any{"minor_units": 1000, "currency": "RUB"}, "item_count": 1, "items": []any{map[string]any{"item_id": "01J00000000000000000000011", "offer_id": "01J00000000000000000000012", "sku": "SKU", "quantity": map[string]any{"value": "1", "unit": "PCS"}, "unit_price": map[string]any{"minor_units": 1000, "currency": "RUB"}, "line_total": map[string]any{"minor_units": 1000, "currency": "RUB"}, "tax": map[string]any{"jurisdiction": "RU", "category": "standard", "rate_fraction": "0.2", "price_includes_tax": true}}}, "version": 2, "change": "status_changed"})
	if err := ingestor.Ingest(context.Background(), []eventbus.Event{first, second}, nil, "backfill-1"); err != nil {
		t.Fatal(err)
	}
	if err := ingestor.Ingest(context.Background(), []eventbus.Event{first, second}, nil, "backfill-1"); err != nil {
		t.Fatal(err)
	}
	scope, _ := tenancy.ParseScope(orgID, wsID)
	service, _ := NewQueryService(projection)
	rows, err := service.Sales(context.Background(), scope, SalesQuery{From: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Currency != "RUB" || rows[0].Orders != 1 || rows[0].FulfilledOrders != 1 || rows[0].GrossMinorUnits != 1000 {
		t.Fatalf("unexpected %+v", rows)
	}
	fresh, _ := service.Freshness(context.Background(), scope)
	if len(fresh) != 1 || fresh[0].EventCount != 2 {
		t.Fatalf("replay duplicated freshness: %+v", fresh)
	}
}

type replaySource struct {
	events []eventbus.Event
	calls  int
}

func (s *replaySource) Page(_ context.Context, _ tenancy.Scope, checkpoint string, limit int) ([]eventbus.Event, string, bool, error) {
	s.calls++
	if checkpoint == "done" {
		return nil, "done", true, nil
	}
	if len(s.events) > limit {
		return s.events[:limit], "next", false, nil
	}
	return s.events, "done", true, nil
}

type checkpointStore struct {
	value string
	done  bool
}

func (s *checkpointStore) Load(context.Context, tenancy.Scope, string) (string, error) {
	return s.value, nil
}
func (s *checkpointStore) Save(_ context.Context, _ tenancy.Scope, _ string, value string, done bool) error {
	s.value = value
	s.done = done
	return nil
}

func TestReplayRunnerCheckpointAndNoProgressGuard(t *testing.T) {
	projection := NewMemoryProjection()
	ingestor, _ := NewIngestor(projection, func() time.Time { return time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC) })
	ev := event(t, "evt-stock-1", "commerce.inventory.stock_changed.v1", "offer-1", "2026-08-10T10:00:00Z", map[string]any{"offer_id": "offer-1", "warehouse_id": "wh-1", "old_quantity": "1", "new_quantity": "2"})
	source := &replaySource{events: []eventbus.Event{ev}}
	checkpoints := &checkpointStore{}
	runner, _ := NewReplayRunner(source, checkpoints, ingestor)
	scope, _ := tenancy.ParseScope(orgID, wsID)
	done, count, err := runner.RunPage(context.Background(), scope, "bf-1", 100)
	if err != nil || !done || count != 1 || checkpoints.value != "done" {
		t.Fatalf("done=%v count=%d cp=%q err=%v", done, count, checkpoints.value, err)
	}
	stalled := &stallSource{}
	runner, _ = NewReplayRunner(stalled, &checkpointStore{}, ingestor)
	if _, _, err := runner.RunPage(context.Background(), scope, "bf-2", 100); !errors.Is(err, ErrReplayStalled) {
		t.Fatalf("err=%v", err)
	}
}

type stallSource struct{}

func (*stallSource) Page(context.Context, tenancy.Scope, string, int) ([]eventbus.Event, string, bool, error) {
	return nil, "", false, nil
}

func TestInventoryUsesLatestEventAndFreshnessLag(t *testing.T) {
	projection := NewMemoryProjection()
	ingestor, _ := NewIngestor(projection, func() time.Time { return time.Date(2026, 8, 10, 10, 5, 0, 0, time.UTC) })
	old := event(t, "evt-stock-1", "commerce.inventory.stock_changed.v1", "offer-1", "2026-08-10T10:00:00Z", map[string]any{"offer_id": "offer-1", "warehouse_id": "wh-1", "old_quantity": "1", "new_quantity": "2"})
	newer := event(t, "evt-stock-2", "commerce.inventory.stock_changed.v1", "offer-1", "2026-08-10T10:01:00Z", map[string]any{"offer_id": "offer-1", "warehouse_id": "wh-1", "old_quantity": "2", "new_quantity": "3.5"})
	if err := ingestor.Ingest(context.Background(), []eventbus.Event{old, newer}, nil, ""); err != nil {
		t.Fatal(err)
	}
	scope, _ := tenancy.ParseScope(orgID, wsID)
	service, _ := NewQueryService(projection)
	rows, err := service.Inventory(context.Background(), scope, InventoryQuery{OfferID: "offer-1", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Quantity != "3.5" || rows[0].EventID != "evt-stock-2" {
		t.Fatalf("unexpected %+v", rows)
	}
	fresh, _ := service.Freshness(context.Background(), scope)
	if len(fresh) != 1 || fresh[0].SourceLag != 4*time.Minute {
		t.Fatalf("unexpected freshness %+v", fresh)
	}
}

func TestEventFactRowsNeverExposeDisallowedPayload(t *testing.T) {
	secretEvent := event(t, "evt-secret-1", "commerce.catalog.product_changed.v1", "product-1", "2026-08-10T10:00:00Z", map[string]any{"private_note": "do-not-copy"})
	record, err := NewRecord(secretEvent, time.Date(2026, 8, 10, 10, 0, 1, 0, time.UTC), SourcePosition{Stream: "commerce.catalog.events", Partition: 1, Offset: 2}, "")
	if err != nil {
		t.Fatal(err)
	}
	batch, err := NewBatch([]Record{record})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := EventFactRows(batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || string(rows[0].AnalyticsDataJSON) != "{}" {
		t.Fatalf("unexpected analytics payload %s", rows[0].AnalyticsDataJSON)
	}
	raw, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "" || containsString(string(raw), "do-not-copy") {
		t.Fatalf("disallowed payload leaked: %s", raw)
	}
}

func containsString(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

type failingSink struct{ err error }

func (s failingSink) Append(context.Context, Batch) error { return s.err }

func TestReplayCheckpointDoesNotAdvanceOnSinkFailure(t *testing.T) {
	ev := event(t, "evt-stock-fail", "commerce.inventory.stock_changed.v1", "offer-1", "2026-08-10T10:00:00Z", map[string]any{"offer_id": "offer-1", "warehouse_id": "wh-1", "old_quantity": "1", "new_quantity": "2"})
	ingestor, _ := NewIngestor(failingSink{err: errors.New("clickhouse unavailable")}, func() time.Time { return time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC) })
	checkpoints := &checkpointStore{}
	runner, _ := NewReplayRunner(&replaySource{events: []eventbus.Event{ev}}, checkpoints, ingestor)
	scope, _ := tenancy.ParseScope(orgID, wsID)
	if _, _, err := runner.RunPage(context.Background(), scope, "bf-fail", 100); err == nil {
		t.Fatal("expected sink failure")
	}
	if checkpoints.value != "" || checkpoints.done {
		t.Fatalf("checkpoint advanced after failure: %+v", checkpoints)
	}
}
