package inbox

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

func TestConsumerValidation(t *testing.T) {
	t.Parallel()
	for _, valid := range []string{"orders.projector.v1", "inventory-sync:v2", "erp_1c.consumer"} {
		if err := ValidateConsumer(valid); err != nil {
			t.Fatalf("ValidateConsumer(%q)=%v", valid, err)
		}
	}
	for _, invalid := range []string{"", "Orders", "consumer with spaces", "-consumer"} {
		if !errors.Is(ValidateConsumer(invalid), ErrInvalidRecord) {
			t.Fatalf("ValidateConsumer(%q) accepted", invalid)
		}
	}
}

func TestFingerprintStableForSameEventAndChangesWithContent(t *testing.T) {
	t.Parallel()
	event := validEvent(t)
	first, err := Fingerprint(event)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Fingerprint(event)
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("fingerprints first=%q second=%q err=%v", first, second, err)
	}
	changed := event
	changed.Data = json.RawMessage(`{"order_id":"order_002"}`)
	other, err := Fingerprint(changed)
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Fatal("different immutable event content produced the same test fingerprint")
	}
}

func TestFingerprintRejectsInvalidEvent(t *testing.T) {
	t.Parallel()
	if _, err := Fingerprint(eventbus.Event{}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("Fingerprint() error=%v", err)
	}
}

func validEvent(t *testing.T) eventbus.Event {
	t.Helper()
	instant, err := domain.NewUTCInstant(time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	typeID, err := eventbus.ParseEventType("commerce.orders.order_created.v1")
	if err != nil {
		t.Fatal(err)
	}
	return eventbus.Event{
		ID:             "evt_inbox_009",
		Type:           typeID,
		OccurredAt:     instant,
		OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001",
		WorkspaceID:    "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002",
		EntityType:     "order",
		EntityID:       "order_001",
		Source:         "orders",
		CorrelationID:  "corr_001",
		Data:           json.RawMessage(`{"order_id":"order_001"}`),
	}
}
