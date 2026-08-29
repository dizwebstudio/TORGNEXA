package kafkaeventbus

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

type fakeProducer struct {
	caps     ProducerCapabilities
	messages []Message
	err      error
}

func (f *fakeProducer) Capabilities() ProducerCapabilities { return f.caps }
func (f *fakeProducer) Produce(_ context.Context, message Message) error {
	if f.err != nil {
		return f.err
	}
	copyMessage := message
	copyMessage.Key = append([]byte(nil), message.Key...)
	copyMessage.Value = append([]byte(nil), message.Value...)
	copyMessage.Headers = append([]Header(nil), message.Headers...)
	f.messages = append(f.messages, copyMessage)
	return nil
}

type fakeReader struct {
	messages  []Message
	committed []Message
	readErr   error
	commitErr error
}

type replayingReader struct {
	message        Message
	reads          int
	commits        int
	firstCommitErr error
}

func (r *replayingReader) Read(ctx context.Context) (Message, error) {
	if r.reads < 2 {
		r.reads++
		return r.message, nil
	}
	<-ctx.Done()
	return Message{}, ctx.Err()
}

func (r *replayingReader) Commit(_ context.Context, _ Message) error {
	r.commits++
	if r.commits == 1 {
		return r.firstCommitErr
	}
	return nil
}

func (f *fakeReader) Read(context.Context) (Message, error) {
	if f.readErr != nil {
		return Message{}, f.readErr
	}
	if len(f.messages) == 0 {
		return Message{}, errors.New("no message")
	}
	m := f.messages[0]
	f.messages = f.messages[1:]
	return m, nil
}
func (f *fakeReader) Commit(_ context.Context, message Message) error {
	if f.commitErr != nil {
		return f.commitErr
	}
	f.committed = append(f.committed, message)
	return nil
}

type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
	err    error
}

func (f *fakeClock) Now() time.Time { return f.now }
func (f *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	if f.err != nil {
		return f.err
	}
	f.sleeps = append(f.sleeps, d)
	f.now = f.now.Add(d)
	return nil
}

func validProducer() *fakeProducer {
	return &fakeProducer{caps: ProducerCapabilities{Idempotent: true, AcksAll: true}}
}

func sampleEvent(t *testing.T) eventbus.Event {
	t.Helper()
	instant, err := domain.NewUTCInstant(time.Date(2026, 8, 9, 9, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	typeName, err := eventbus.ParseEventType("commerce.orders.order_created.v1")
	if err != nil {
		t.Fatal(err)
	}
	return eventbus.Event{
		ID:             "evt_test_007_kafka",
		Type:           typeName,
		OccurredAt:     instant,
		OrganizationID: "org_test_001",
		WorkspaceID:    "workspace_test_001",
		EntityType:     "order",
		EntityID:       "order_test_001",
		Source:         "orders",
		Data:           json.RawMessage(`{"order_id":"order_test_001"}`),
	}
}

func publishedMessage(t *testing.T, event eventbus.Event) Message {
	t.Helper()
	producer := validProducer()
	publisher, err := NewPublisher(producer)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(producer.messages) != 1 {
		t.Fatalf("messages=%d", len(producer.messages))
	}
	return producer.messages[0]
}

func newTestConsumer(t *testing.T, reader Reader, producer *fakeProducer, clock Clock, maxAttempts uint16) *Consumer {
	t.Helper()
	consumer, err := NewConsumer(reader, producer, []string{"commerce.orders.events.v1"}, RetryPolicy{
		MaxAttempts: maxAttempts, InitialBackoff: time.Second, MaxBackoff: 8 * time.Second,
	}, clock)
	if err != nil {
		t.Fatal(err)
	}
	return consumer
}

func TestPublisherRequiresSafeKafkaProducer(t *testing.T) {
	for name, caps := range map[string]ProducerCapabilities{
		"no idempotence": {AcksAll: true},
		"not all acks":   {Idempotent: true},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewPublisher(&fakeProducer{caps: caps}); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestPublisherMapsCanonicalTopicKeyAndHeaders(t *testing.T) {
	event := sampleEvent(t)
	message := publishedMessage(t, event)
	if message.Topic != "commerce.orders.events.v1" {
		t.Fatalf("topic=%q", message.Topic)
	}
	if len(message.Key) != 32 {
		t.Fatalf("partition key bytes=%d", len(message.Key))
	}
	if !message.Timestamp.Equal(event.OccurredAt.Time()) {
		t.Fatalf("timestamp=%s", message.Timestamp)
	}
	if got := mustHeader(t, message.Headers, HeaderEventID); got != event.ID {
		t.Fatalf("event id header=%q", got)
	}
	if got := mustHeader(t, message.Headers, HeaderEventType); got != event.Type.String() {
		t.Fatalf("event type header=%q", got)
	}
	if got := mustHeader(t, message.Headers, HeaderContentType); got != ContentTypeJSON {
		t.Fatalf("content type=%q", got)
	}
	var decoded eventbus.Event
	if err := json.Unmarshal(message.Value, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != event.ID {
		t.Fatalf("decoded event=%q", decoded.ID)
	}
}

func TestPublisherDoesNotLeakProducerError(t *testing.T) {
	producer := validProducer()
	producer.err = errors.New("broker auth failed token=secret-value")
	publisher, err := NewPublisher(producer)
	if err != nil {
		t.Fatal(err)
	}
	err = publisher.Publish(context.Background(), sampleEvent(t))
	if err == nil {
		t.Fatal("expected failure")
	}
	if strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("secret leaked: %v", err)
	}
}

func TestPartitionKeyIsTenantAndAggregateBound(t *testing.T) {
	event := sampleEvent(t)
	key1, err := PartitionKey(event)
	if err != nil {
		t.Fatal(err)
	}
	event.OrganizationID = "org_test_002"
	key2, err := PartitionKey(event)
	if err != nil {
		t.Fatal(err)
	}
	if string(key1) == string(key2) {
		t.Fatal("tenant change must change partition key")
	}
}

func TestConsumerSuccessCommitsWithoutRepublish(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)}
	message := publishedMessage(t, sampleEvent(t))
	reader := &fakeReader{}
	producer := validProducer()
	consumer := newTestConsumer(t, reader, producer, clock, 3)
	called := 0
	err := consumer.HandleOne(context.Background(), message, func(_ context.Context, delivery eventbus.Delivery) error {
		called++
		if delivery.Attempt != 1 {
			t.Fatalf("attempt=%d", delivery.Attempt)
		}
		if delivery.Event.ID != "evt_test_007_kafka" {
			t.Fatalf("event=%q", delivery.Event.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if called != 1 || len(reader.committed) != 1 || len(producer.messages) != 0 {
		t.Fatalf("called=%d committed=%d produced=%d", called, len(reader.committed), len(producer.messages))
	}
}

func TestConsumerRunRetriesCommitFailureWithoutStoppingWorker(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)}
	message := publishedMessage(t, sampleEvent(t))
	reader := &replayingReader{message: message, firstCommitErr: errors.New("broker temporarily unavailable")}
	consumer := newTestConsumer(t, reader, validProducer(), clock, 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	err := consumer.Run(ctx, func(_ context.Context, _ eventbus.Delivery) error {
		calls++
		if calls == 2 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run error=%v", err)
	}
	if calls != 2 || reader.commits != 2 || len(clock.sleeps) != 1 {
		t.Fatalf("calls=%d commits=%d sleeps=%d", calls, reader.commits, len(clock.sleeps))
	}
}

func TestConsumerRetryPublishesBeforeCommitWithSafeMetadata(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)}
	message := publishedMessage(t, sampleEvent(t))
	reader := &fakeReader{}
	producer := validProducer()
	consumer := newTestConsumer(t, reader, producer, clock, 4)
	err := consumer.HandleOne(context.Background(), message, func(context.Context, eventbus.Delivery) error {
		return errors.New("Authorization: Bearer secret-retry-token")
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(producer.messages) != 1 || len(reader.committed) != 1 {
		t.Fatalf("produced=%d committed=%d", len(producer.messages), len(reader.committed))
	}
	retry := producer.messages[0]
	if retry.Topic != "commerce.orders.events.v1.retry" {
		t.Fatalf("topic=%q", retry.Topic)
	}
	if got := mustHeader(t, retry.Headers, HeaderAttempt); got != "2" {
		t.Fatalf("attempt=%q", got)
	}
	if got := mustHeader(t, retry.Headers, HeaderFailureCode); got != "handler_error" {
		t.Fatalf("code=%q", got)
	}
	if got := mustHeader(t, retry.Headers, HeaderNotBefore); got != "2026-08-09T10:00:01Z" {
		t.Fatalf("not_before=%q", got)
	}
	for _, h := range retry.Headers {
		if strings.Contains(string(h.Value), "secret-retry-token") {
			t.Fatalf("secret leaked in header %q", h.Key)
		}
	}
}

func TestConsumerRetryRecordHonorsNotBeforeAndAttempt(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)}
	base := publishedMessage(t, sampleEvent(t))
	firstReader := &fakeReader{}
	firstProducer := validProducer()
	first := newTestConsumer(t, firstReader, firstProducer, clock, 4)
	if err := first.HandleOne(context.Background(), base, func(context.Context, eventbus.Delivery) error { return eventbus.Retryable("temporary_dependency") }); err != nil {
		t.Fatal(err)
	}
	retry := firstProducer.messages[0]

	reader := &fakeReader{}
	producer := validProducer()
	consumer := newTestConsumer(t, reader, producer, clock, 4)
	called := 0
	if err := consumer.HandleOne(context.Background(), retry, func(_ context.Context, delivery eventbus.Delivery) error {
		called++
		if delivery.Attempt != 2 {
			t.Fatalf("attempt=%d", delivery.Attempt)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if called != 1 || len(clock.sleeps) != 1 || clock.sleeps[0] != time.Second {
		t.Fatalf("called=%d sleeps=%v", called, clock.sleeps)
	}
	if len(reader.committed) != 1 {
		t.Fatalf("committed=%d", len(reader.committed))
	}
}

func TestConsumerPermanentAndExhaustedRouteToDLQ(t *testing.T) {
	cases := []struct {
		name    string
		max     uint16
		handler eventbus.Handler
		reason  string
		code    string
	}{
		{name: "permanent", max: 3, handler: func(context.Context, eventbus.Delivery) error { return eventbus.Permanent("schema_rejected") }, reason: TerminalPermanent, code: "schema_rejected"},
		{name: "exhausted", max: 1, handler: func(context.Context, eventbus.Delivery) error { return eventbus.Retryable("dependency_down") }, reason: TerminalRetryExhausted, code: "dependency_down"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := &fakeClock{now: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)}
			reader := &fakeReader{}
			producer := validProducer()
			consumer := newTestConsumer(t, reader, producer, clock, tc.max)
			if err := consumer.HandleOne(context.Background(), publishedMessage(t, sampleEvent(t)), tc.handler); err != nil {
				t.Fatal(err)
			}
			if len(producer.messages) != 1 || len(reader.committed) != 1 {
				t.Fatalf("produced=%d committed=%d", len(producer.messages), len(reader.committed))
			}
			dlq := producer.messages[0]
			if dlq.Topic != "commerce.orders.events.v1.dlq" {
				t.Fatalf("topic=%q", dlq.Topic)
			}
			if got := mustHeader(t, dlq.Headers, HeaderTerminalReason); got != tc.reason {
				t.Fatalf("reason=%q", got)
			}
			if got := mustHeader(t, dlq.Headers, HeaderFailureCode); got != tc.code {
				t.Fatalf("code=%q", got)
			}
		})
	}
}

func TestConsumerInvalidEventAndTopicMismatchRouteToDLQ(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)}
	cases := []struct {
		name    string
		message Message
		reason  string
	}{
		{name: "invalid", message: func() Message {
			m := publishedMessage(t, sampleEvent(t))
			m.Value = []byte(`{"secret":"not-an-envelope"}`)
			return m
		}(), reason: TerminalInvalidEvent},
		{name: "topic mismatch", message: func() Message {
			m := publishedMessage(t, sampleEvent(t))
			m.Topic = "commerce.inventory.events.v1"
			return m
		}(), reason: TerminalTopicMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeReader{}
			producer := validProducer()
			baseTopics := []string{"commerce.orders.events.v1"}
			if tc.name == "topic mismatch" {
				baseTopics = append(baseTopics, "commerce.inventory.events.v1")
			}
			consumer, err := NewConsumer(reader, producer, baseTopics, RetryPolicy{MaxAttempts: 3, InitialBackoff: time.Second, MaxBackoff: time.Second}, clock)
			if err != nil {
				t.Fatal(err)
			}
			called := false
			if err := consumer.HandleOne(context.Background(), tc.message, func(context.Context, eventbus.Delivery) error { called = true; return nil }); err != nil {
				t.Fatal(err)
			}
			if called {
				t.Fatal("handler must not receive invalid transport record")
			}
			if len(producer.messages) != 1 || len(reader.committed) != 1 {
				t.Fatalf("produced=%d committed=%d", len(producer.messages), len(reader.committed))
			}
			if got := mustHeader(t, producer.messages[0].Headers, HeaderTerminalReason); got != tc.reason {
				t.Fatalf("reason=%q", got)
			}
		})
	}
}

func TestConsumerHeaderEnvelopeMismatchRoutesToDLQ(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)}
	message := publishedMessage(t, sampleEvent(t))
	for i := range message.Headers {
		if message.Headers[i].Key == HeaderEventID {
			message.Headers[i].Value = []byte("evt_tampered")
		}
	}
	reader := &fakeReader{}
	producer := validProducer()
	consumer := newTestConsumer(t, reader, producer, clock, 3)
	if err := consumer.HandleOne(context.Background(), message, func(context.Context, eventbus.Delivery) error { t.Fatal("handler called"); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(producer.messages) != 1 || len(reader.committed) != 1 {
		t.Fatalf("produced=%d committed=%d", len(producer.messages), len(reader.committed))
	}
	if got := mustHeader(t, producer.messages[0].Headers, HeaderTerminalReason); got != TerminalHeaderMismatch {
		t.Fatalf("reason=%q", got)
	}
}

func TestConsumerDoesNotCommitWhenRetryOrDLQPublishFails(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)}
	for name, handler := range map[string]eventbus.Handler{
		"retry": func(context.Context, eventbus.Delivery) error { return eventbus.Retryable("temporary") },
		"dlq":   func(context.Context, eventbus.Delivery) error { return eventbus.Permanent("invalid") },
	} {
		t.Run(name, func(t *testing.T) {
			reader := &fakeReader{}
			producer := validProducer()
			producer.err = errors.New("broker secret=password")
			consumer := newTestConsumer(t, reader, producer, clock, 3)
			err := consumer.HandleOne(context.Background(), publishedMessage(t, sampleEvent(t)), handler)
			if err == nil {
				t.Fatal("expected publish failure")
			}
			if strings.Contains(err.Error(), "password") {
				t.Fatalf("broker error leaked: %v", err)
			}
			if len(reader.committed) != 0 {
				t.Fatalf("committed=%d", len(reader.committed))
			}
		})
	}
}

func TestConsumerRejectsMalformedRetryMetadataIntoDLQ(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)}
	message := publishedMessage(t, sampleEvent(t))
	message.Topic += RetryTopicSuffix
	message.Headers = append(message.Headers,
		Header{Key: HeaderAttempt, Value: []byte("2")},
		Header{Key: HeaderAttempt, Value: []byte("3")},
		Header{Key: HeaderOriginalTopic, Value: []byte("commerce.orders.events.v1")},
	)
	reader := &fakeReader{}
	producer := validProducer()
	consumer := newTestConsumer(t, reader, producer, clock, 3)
	if err := consumer.HandleOne(context.Background(), message, func(context.Context, eventbus.Delivery) error { t.Fatal("handler called"); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(producer.messages) != 1 || len(reader.committed) != 1 {
		t.Fatalf("produced=%d committed=%d", len(producer.messages), len(reader.committed))
	}
	if got := mustHeader(t, producer.messages[0].Headers, HeaderTerminalReason); got != TerminalMetadata {
		t.Fatalf("reason=%q", got)
	}
}

func TestConsumerCancelDuringRetryDelayDoesNotCommit(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC), err: context.Canceled}
	message := publishedMessage(t, sampleEvent(t))
	first, _ := domain.NewUTCInstant(clock.now.Add(-time.Minute))
	notBefore, _ := domain.NewUTCInstant(clock.now.Add(time.Minute))
	message.Topic += RetryTopicSuffix
	message.Headers = append(message.Headers,
		Header{Key: HeaderAttempt, Value: []byte("2")},
		Header{Key: HeaderFirstObservedAt, Value: []byte(first.String())},
		Header{Key: HeaderOriginalTopic, Value: []byte("commerce.orders.events.v1")},
		Header{Key: HeaderNotBefore, Value: []byte(notBefore.String())},
	)
	reader := &fakeReader{}
	producer := validProducer()
	consumer := newTestConsumer(t, reader, producer, clock, 3)
	if err := consumer.HandleOne(context.Background(), message, func(context.Context, eventbus.Delivery) error { t.Fatal("handler called"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if len(reader.committed) != 0 || len(producer.messages) != 0 {
		t.Fatalf("committed=%d produced=%d", len(reader.committed), len(producer.messages))
	}
}

func TestTopicsIncludeBaseAndRetryOnly(t *testing.T) {
	consumer := newTestConsumer(t, &fakeReader{}, validProducer(), &fakeClock{now: time.Now().UTC()}, 3)
	got := consumer.Topics()
	if len(got) != 2 || got[0] != "commerce.orders.events.v1" || got[1] != "commerce.orders.events.v1.retry" {
		t.Fatalf("topics=%v", got)
	}
}

func TestRetryPolicyBackoffCaps(t *testing.T) {
	policy := RetryPolicy{MaxAttempts: 10, InitialBackoff: time.Second, MaxBackoff: 4 * time.Second}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second}
	for i, expected := range want {
		if got := policy.Backoff(uint16(i + 1)); got != expected {
			t.Fatalf("attempt=%d got=%s want=%s", i+1, got, expected)
		}
	}
}

func mustHeader(t *testing.T, headers []Header, key string) string {
	t.Helper()
	value, ok, err := uniqueHeader(headers, key)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("missing header %q", key)
	}
	return string(value)
}
