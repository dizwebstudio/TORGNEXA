package eventbus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

const (
	MaxEventDataBytes = 1 << 20
	MaxEventJSONDepth = 64
	MaxEventJSONKeys  = 10000
)

var (
	eventTypePattern  = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.v[1-9][0-9]{0,2}$`)
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	sourcePattern     = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)
	errorCodePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// EventType is the canonical versioned event identifier, for example
// commerce.orders.order_created.v1. Its first two segments form the transport
// family and the final segment is the immutable schema version.
type EventType string

func ParseEventType(value string) (EventType, error) {
	if !eventTypePattern.MatchString(value) {
		return "", errors.New("event type must use <namespace>.<domain>.<event>.v<1-999>")
	}
	return EventType(value), nil
}

func (e EventType) String() string { return string(e) }
func (e EventType) Validate() error {
	_, err := ParseEventType(string(e))
	return err
}

func (e EventType) Family() (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	parts := strings.Split(string(e), ".")
	return parts[0] + "." + parts[1], nil
}

func (e EventType) Version() (uint16, error) {
	if err := e.Validate(); err != nil {
		return 0, err
	}
	parts := strings.Split(string(e), ".")
	parsed, err := strconv.ParseUint(strings.TrimPrefix(parts[len(parts)-1], "v"), 10, 16)
	if err != nil || parsed == 0 || parsed > 999 {
		return 0, errors.New("event type has invalid version")
	}
	return uint16(parsed), nil
}

func (e EventType) MarshalText() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return []byte(e), nil
}

func (e *EventType) UnmarshalText(data []byte) error {
	parsed, err := ParseEventType(string(data))
	if err != nil {
		return err
	}
	*e = parsed
	return nil
}

// Event is the broker-neutral canonical envelope. Data must be a JSON object;
// it is retained verbatim so schema-specific payload validation can happen at
// the owning domain boundary without binary-float or map re-encoding drift.
type Event struct {
	ID             string
	Type           EventType
	OccurredAt     domain.UTCInstant
	OrganizationID string
	WorkspaceID    string
	EntityType     string
	EntityID       string
	Source         string
	CorrelationID  string
	CausationID    string
	ActorID        string
	TraceID        string
	Data           json.RawMessage
}

func (e Event) Validate() error {
	if err := validateIdentifier("event_id", e.ID, true); err != nil {
		return err
	}
	if err := e.Type.Validate(); err != nil {
		return fmt.Errorf("event_type: %w", err)
	}
	if err := e.OccurredAt.Validate(); err != nil {
		return fmt.Errorf("occurred_at: %w", err)
	}
	if err := validateIdentifier("organization_id", e.OrganizationID, true); err != nil {
		return err
	}
	if err := validateIdentifier("workspace_id", e.WorkspaceID, true); err != nil {
		return err
	}
	if err := validateIdentifier("entity_type", e.EntityType, true); err != nil {
		return err
	}
	if err := validateIdentifier("entity_id", e.EntityID, true); err != nil {
		return err
	}
	if !sourcePattern.MatchString(e.Source) {
		return errors.New("source must be a non-empty canonical source identifier")
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "correlation_id", value: e.CorrelationID},
		{name: "causation_id", value: e.CausationID},
		{name: "actor_id", value: e.ActorID},
		{name: "trace_id", value: e.TraceID},
	} {
		if err := validateIdentifier(field.name, field.value, false); err != nil {
			return err
		}
	}
	if err := ValidateData(e.Data); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	return nil
}

func validateIdentifier(name, value string, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%s is required", name)
		}
		return nil
	}
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("%s is not a canonical identifier", name)
	}
	return nil
}

func ValidateData(data json.RawMessage) error {
	if len(data) == 0 {
		return errors.New("event data is required")
	}
	if len(data) > MaxEventDataBytes {
		return fmt.Errorf("event data exceeds %d bytes", MaxEventDataBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	first, err := dec.Token()
	if err != nil {
		return errors.New("event data is invalid JSON")
	}
	delim, ok := first.(json.Delim)
	if !ok || delim != '{' {
		return errors.New("event data must be a JSON object")
	}
	keys := 0
	if err := validateJSONObjectTokens(dec, 1, &keys, map[string]struct{}{}); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return errors.New("event data contains trailing JSON")
		}
		return errors.New("event data is invalid JSON")
	}
	return nil
}

func validateJSONObjectTokens(dec *json.Decoder, depth int, keys *int, seen map[string]struct{}) error {
	if depth > MaxEventJSONDepth {
		return fmt.Errorf("event data exceeds maximum JSON depth %d", MaxEventJSONDepth)
	}
	for dec.More() {
		rawKey, err := dec.Token()
		if err != nil {
			return errors.New("event data is invalid JSON object")
		}
		key, ok := rawKey.(string)
		if !ok {
			return errors.New("event data has invalid JSON object key")
		}
		if _, duplicate := seen[key]; duplicate {
			return errors.New("event data contains duplicate JSON object key")
		}
		seen[key] = struct{}{}
		(*keys)++
		if *keys > MaxEventJSONKeys {
			return fmt.Errorf("event data exceeds maximum JSON key count %d", MaxEventJSONKeys)
		}
		if err := validateJSONValue(dec, depth+1, keys); err != nil {
			return err
		}
	}
	end, err := dec.Token()
	if err != nil || end != json.Delim('}') {
		return errors.New("event data has invalid JSON object termination")
	}
	return nil
}

func validateJSONValue(dec *json.Decoder, depth int, keys *int) error {
	if depth > MaxEventJSONDepth {
		return fmt.Errorf("event data exceeds maximum JSON depth %d", MaxEventJSONDepth)
	}
	token, err := dec.Token()
	if err != nil {
		return errors.New("event data is invalid JSON value")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		return validateJSONObjectTokens(dec, depth, keys, map[string]struct{}{})
	case '[':
		for dec.More() {
			if err := validateJSONValue(dec, depth+1, keys); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("event data has invalid JSON array termination")
		}
		return nil
	default:
		return errors.New("event data contains unexpected JSON delimiter")
	}
}

func (e Event) MarshalJSON() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		EventID        string            `json:"event_id"`
		EventType      EventType         `json:"event_type"`
		OccurredAt     domain.UTCInstant `json:"occurred_at"`
		OrganizationID string            `json:"organization_id"`
		WorkspaceID    string            `json:"workspace_id"`
		CorrelationID  *string           `json:"correlation_id"`
		CausationID    *string           `json:"causation_id"`
		EntityType     string            `json:"entity_type"`
		EntityID       string            `json:"entity_id"`
		Source         string            `json:"source"`
		ActorID        *string           `json:"actor_id,omitempty"`
		TraceID        *string           `json:"trace_id,omitempty"`
		Data           json.RawMessage   `json:"data"`
	}{
		EventID:        e.ID,
		EventType:      e.Type,
		OccurredAt:     e.OccurredAt,
		OrganizationID: e.OrganizationID,
		WorkspaceID:    e.WorkspaceID,
		CorrelationID:  nullableString(e.CorrelationID),
		CausationID:    nullableString(e.CausationID),
		EntityType:     e.EntityType,
		EntityID:       e.EntityID,
		Source:         e.Source,
		ActorID:        nullableString(e.ActorID),
		TraceID:        nullableString(e.TraceID),
		Data:           e.Data,
	})
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func (e *Event) UnmarshalJSON(data []byte) error {
	if len(data) > MaxEventDataBytes+32*1024 {
		return errors.New("event envelope exceeds maximum size")
	}
	fields, err := decodeUniqueObject(data)
	if err != nil {
		return err
	}
	allowed := map[string]struct{}{
		"event_id": {}, "event_type": {}, "occurred_at": {}, "organization_id": {},
		"workspace_id": {}, "correlation_id": {}, "causation_id": {}, "entity_type": {},
		"entity_id": {}, "source": {}, "actor_id": {}, "trace_id": {}, "data": {},
	}
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("event envelope contains unknown field %q", key)
		}
	}
	required := []string{"event_id", "event_type", "occurred_at", "organization_id", "workspace_id", "correlation_id", "causation_id", "entity_type", "entity_id", "source", "data"}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("event envelope missing required field %q", key)
		}
	}
	var out Event
	if out.ID, err = decodeStringField(fields, "event_id"); err != nil {
		return err
	}
	var eventType string
	if eventType, err = decodeStringField(fields, "event_type"); err != nil {
		return err
	}
	if out.Type, err = ParseEventType(eventType); err != nil {
		return fmt.Errorf("event_type: %w", err)
	}
	var occurred string
	if occurred, err = decodeStringField(fields, "occurred_at"); err != nil {
		return err
	}
	if out.OccurredAt, err = domain.ParseUTCInstant(occurred); err != nil {
		return fmt.Errorf("occurred_at: %w", err)
	}
	if out.OrganizationID, err = decodeStringField(fields, "organization_id"); err != nil {
		return err
	}
	if out.WorkspaceID, err = decodeStringField(fields, "workspace_id"); err != nil {
		return err
	}
	if out.CorrelationID, err = decodeNullableStringField(fields, "correlation_id"); err != nil {
		return err
	}
	if out.CausationID, err = decodeNullableStringField(fields, "causation_id"); err != nil {
		return err
	}
	if out.EntityType, err = decodeStringField(fields, "entity_type"); err != nil {
		return err
	}
	if out.EntityID, err = decodeStringField(fields, "entity_id"); err != nil {
		return err
	}
	if out.Source, err = decodeStringField(fields, "source"); err != nil {
		return err
	}
	if _, ok := fields["actor_id"]; ok {
		if out.ActorID, err = decodeNullableStringField(fields, "actor_id"); err != nil {
			return err
		}
	}
	if _, ok := fields["trace_id"]; ok {
		if out.TraceID, err = decodeNullableStringField(fields, "trace_id"); err != nil {
			return err
		}
	}
	out.Data = append(json.RawMessage(nil), fields["data"]...)
	if err := out.Validate(); err != nil {
		return err
	}
	*e = out
	return nil
}

func decodeUniqueObject(data []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return nil, errors.New("event envelope must be a JSON object")
	}
	out := make(map[string]json.RawMessage)
	for dec.More() {
		rawKey, err := dec.Token()
		if err != nil {
			return nil, errors.New("event envelope has invalid key")
		}
		key, ok := rawKey.(string)
		if !ok {
			return nil, errors.New("event envelope has invalid key")
		}
		if _, duplicate := out[key]; duplicate {
			return nil, errors.New("event envelope contains duplicate field")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, errors.New("event envelope has invalid field value")
		}
		out[key] = append(json.RawMessage(nil), raw...)
	}
	end, err := dec.Token()
	if err != nil || end != json.Delim('}') {
		return nil, errors.New("event envelope has invalid termination")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("event envelope contains trailing JSON")
	}
	return out, nil
}

func decodeStringField(fields map[string]json.RawMessage, name string) (string, error) {
	var value string
	if err := json.Unmarshal(fields[name], &value); err != nil {
		return "", fmt.Errorf("event envelope field %q must be a string", name)
	}
	return value, nil
}

func decodeNullableStringField(fields map[string]json.RawMessage, name string) (string, error) {
	if bytes.Equal(bytes.TrimSpace(fields[name]), []byte("null")) {
		return "", nil
	}
	return decodeStringField(fields, name)
}

// Delivery exposes broker-neutral delivery metadata to a consumer. Attempt is
// one-based; the same immutable Event.ID is preserved across retries.
type Delivery struct {
	Event           Event
	Attempt         uint16
	FirstObservedAt domain.UTCInstant
}

func (d Delivery) Validate() error {
	if err := d.Event.Validate(); err != nil {
		return err
	}
	if d.Attempt == 0 {
		return errors.New("delivery attempt must be at least 1")
	}
	if err := d.FirstObservedAt.Validate(); err != nil {
		return fmt.Errorf("first observed at: %w", err)
	}
	return nil
}

type Publisher interface {
	Publish(context.Context, Event) error
}

type Handler func(context.Context, Delivery) error

type Consumer interface {
	Run(context.Context, Handler) error
}

type EventBus interface {
	Publisher
	Consumer
}

type FailureClass uint8

const (
	FailureRetryable FailureClass = iota + 1
	FailurePermanent
)

// DeliveryError carries a bounded machine code only. It intentionally does not
// wrap arbitrary handler text because errors commonly contain tokens or PII.
type DeliveryError struct {
	class FailureClass
	code  string
}

func Retryable(code string) error { return newDeliveryError(FailureRetryable, code) }
func Permanent(code string) error { return newDeliveryError(FailurePermanent, code) }

func newDeliveryError(class FailureClass, code string) error {
	if !errorCodePattern.MatchString(code) {
		return &DeliveryError{class: class, code: "invalid_error_code"}
	}
	return &DeliveryError{class: class, code: code}
}

func (e *DeliveryError) Error() string       { return "event delivery failed: " + e.code }
func (e *DeliveryError) Class() FailureClass { return e.class }
func (e *DeliveryError) Code() string        { return e.code }

// ClassifyFailure treats unknown handler errors as retryable with a fixed code,
// preventing arbitrary error text from crossing the broker boundary.
func ClassifyFailure(err error) (FailureClass, string) {
	if err == nil {
		return 0, ""
	}
	var classified *DeliveryError
	if errors.As(err, &classified) {
		return classified.class, classified.code
	}
	return FailureRetryable, "handler_error"
}
