package eventbus

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

func testEvent(t *testing.T) Event {
	t.Helper()
	instant, err := domain.NewUTCInstant(time.Date(2026, 8, 9, 9, 0, 0, 123, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	typeName, err := ParseEventType("commerce.orders.order_created.v1")
	if err != nil {
		t.Fatal(err)
	}
	return Event{
		ID:             "evt_test_007",
		Type:           typeName,
		OccurredAt:     instant,
		OrganizationID: "org_test_001",
		WorkspaceID:    "workspace_test_001",
		EntityType:     "order",
		EntityID:       "order_test_001",
		Source:         "orders",
		CorrelationID:  "corr_test_001",
		CausationID:    "cause_test_001",
		ActorID:        "actor_test_001",
		TraceID:        "trace_test_001",
		Data:           json.RawMessage(`{"order_id":"order_test_001","quantity":"1.25"}`),
	}
}

func TestEventTypeFamilyVersion(t *testing.T) {
	value, err := ParseEventType("commerce.orders.order_created.v17")
	if err != nil {
		t.Fatal(err)
	}
	family, err := value.Family()
	if err != nil {
		t.Fatal(err)
	}
	if family != "commerce.orders" {
		t.Fatalf("family=%q", family)
	}
	version, err := value.Version()
	if err != nil {
		t.Fatal(err)
	}
	if version != 17 {
		t.Fatalf("version=%d", version)
	}

	for _, input := range []string{
		"commerce.orders.created", "Commerce.orders.created.v1", "commerce.orders.created.v0",
		"commerce.orders.created.v1000", "commerce.orders.created.v01", "commerce.orders.created.extra.v1",
	} {
		if _, err := ParseEventType(input); err == nil {
			t.Fatalf("expected %q to fail", input)
		}
	}
}

func TestEventJSONRoundTripIsCanonical(t *testing.T) {
	original := testEvent(t)
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"occurred_at":"2026-08-09T09:00:00.000000123Z"`) {
		t.Fatalf("UTC instant not canonical: %s", encoded)
	}
	var decoded Event
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != original.ID || decoded.Type != original.Type || decoded.CorrelationID != original.CorrelationID {
		t.Fatalf("roundtrip mismatch: %#v", decoded)
	}
	if string(decoded.Data) != string(original.Data) {
		t.Fatalf("payload changed: %s", decoded.Data)
	}
}

func TestEventJSONUsesRequiredNullCorrelationFields(t *testing.T) {
	event := testEvent(t)
	event.CorrelationID = ""
	event.CausationID = ""
	event.ActorID = ""
	event.TraceID = ""
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["correlation_id"]) != "null" || string(fields["causation_id"]) != "null" {
		t.Fatalf("required nullable fields missing: %s", encoded)
	}
	if _, ok := fields["actor_id"]; ok {
		t.Fatalf("optional actor_id should be omitted: %s", encoded)
	}
	if _, ok := fields["trace_id"]; ok {
		t.Fatalf("optional trace_id should be omitted: %s", encoded)
	}
}

func TestEventUnmarshalRejectsUnsafeEnvelope(t *testing.T) {
	base, err := json.Marshal(testEvent(t))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"unknown field":             strings.TrimSuffix(string(base), "}") + `,"unexpected":true}`,
		"duplicate field":           strings.Replace(string(base), `"event_id":"evt_test_007"`, `"event_id":"evt_test_007","event_id":"evt_other"`, 1),
		"missing nullable required": strings.Replace(string(base), `"correlation_id":"corr_test_001",`, "", 1),
		"non UTC":                   strings.Replace(string(base), `2026-08-09T09:00:00.000000123Z`, `2026-08-09T11:00:00+02:00`, 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var event Event
			if err := json.Unmarshal([]byte(raw), &event); err == nil {
				t.Fatalf("expected rejection: %s", raw)
			}
		})
	}
}

func TestValidateDataRejectsDuplicatesDepthAndNonObject(t *testing.T) {
	for name, raw := range map[string]string{
		"duplicate": `{"a":1,"nested":{"b":1,"b":2}}`,
		"array":     `[1,2,3]`,
		"trailing":  `{} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateData(json.RawMessage(raw)); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}

	deep := strings.Repeat(`{"x":`, MaxEventJSONDepth+1) + `1` + strings.Repeat(`}`, MaxEventJSONDepth+1)
	if err := ValidateData(json.RawMessage(deep)); err == nil {
		t.Fatal("expected depth rejection")
	}
}

func TestValidateDataRejectsOversize(t *testing.T) {
	raw := append([]byte(`{"x":"`), bytesOf('a', MaxEventDataBytes)...)
	raw = append(raw, []byte(`"}`)...)
	if err := ValidateData(raw); err == nil {
		t.Fatal("expected oversize rejection")
	}
}

func bytesOf(value byte, count int) []byte {
	out := make([]byte, count)
	for i := range out {
		out[i] = value
	}
	return out
}

func TestDeliveryValidation(t *testing.T) {
	event := testEvent(t)
	delivery := Delivery{Event: event, Attempt: 1, FirstObservedAt: event.OccurredAt}
	if err := delivery.Validate(); err != nil {
		t.Fatal(err)
	}
	delivery.Attempt = 0
	if err := delivery.Validate(); err == nil {
		t.Fatal("expected zero attempt rejection")
	}
}

func TestFailureClassificationNeverLeaksUnknownError(t *testing.T) {
	class, code := ClassifyFailure(errors.New("Authorization: Bearer secret-token"))
	if class != FailureRetryable || code != "handler_error" {
		t.Fatalf("class=%v code=%q", class, code)
	}
	if strings.Contains(code, "secret") {
		t.Fatal("secret leaked")
	}

	class, code = ClassifyFailure(Permanent("payload_invalid"))
	if class != FailurePermanent || code != "payload_invalid" {
		t.Fatalf("class=%v code=%q", class, code)
	}

	_, code = ClassifyFailure(Retryable("BAD CODE"))
	if code != "invalid_error_code" {
		t.Fatalf("code=%q", code)
	}
}
