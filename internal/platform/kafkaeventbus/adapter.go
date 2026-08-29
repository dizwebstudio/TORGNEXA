package kafkaeventbus

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

const (
	HeaderContentType      = "content-type"
	HeaderEventID          = "x-torgnexa-event-id"
	HeaderEventType        = "x-torgnexa-event-type"
	HeaderAttempt          = "x-torgnexa-attempt"
	HeaderFirstObservedAt  = "x-torgnexa-first-observed-at"
	HeaderOriginalTopic    = "x-torgnexa-original-topic"
	HeaderNotBefore        = "x-torgnexa-not-before"
	HeaderFailureCode      = "x-torgnexa-failure-code"
	HeaderTerminalReason   = "x-torgnexa-terminal-reason"
	ContentTypeJSON        = "application/json"
	RetryTopicSuffix       = ".retry"
	DeadLetterTopicSuffix  = ".dlq"
	TerminalPermanent      = "permanent"
	TerminalRetryExhausted = "retry_exhausted"
	TerminalInvalidEvent   = "invalid_event"
	TerminalTopicMismatch  = "topic_event_mismatch"
	TerminalHeaderMismatch = "envelope_header_mismatch"
	TerminalMetadata       = "invalid_transport_metadata"
	maxHeaderValueBytes    = 4096
	maxSystemHeaderBytes   = 16 * 1024
)

// Message is the minimal Kafka record shape used by the adapter. A concrete
// Kafka client shim owns broker connection/authentication and maps its record
// type to this shape; domain code never imports a Kafka library.
type Message struct {
	Topic     string
	Key       []byte
	Value     []byte
	Headers   []Header
	Timestamp time.Time
	Opaque    any
}

type Header struct {
	Key   string
	Value []byte
}

type ProducerCapabilities struct {
	Idempotent bool
	AcksAll    bool
}

type Producer interface {
	Capabilities() ProducerCapabilities
	Produce(context.Context, Message) error
}

type Reader interface {
	Read(context.Context) (Message, error)
	Commit(context.Context, Message) error
}

var (
	_ eventbus.Publisher = (*Publisher)(nil)
	_ eventbus.Consumer  = (*Consumer)(nil)
)

func validateProducer(producer Producer) error {
	if producer == nil {
		return errors.New("Kafka producer is required")
	}
	caps := producer.Capabilities()
	if !caps.Idempotent || !caps.AcksAll {
		return errors.New("Kafka producer must enable idempotence and acknowledgements from all in-sync replicas")
	}
	return nil
}

func TopicForEventType(eventType eventbus.EventType) (string, error) {
	family, err := eventType.Family()
	if err != nil {
		return "", err
	}
	version, err := eventType.Version()
	if err != nil {
		return "", err
	}
	topic := fmt.Sprintf("%s.events.v%d", family, version)
	if err := ValidateTopic(topic); err != nil {
		return "", err
	}
	return topic, nil
}

func RetryTopic(base string) (string, error) {
	if err := ValidateTopic(base); err != nil {
		return "", err
	}
	if strings.HasSuffix(base, RetryTopicSuffix) || strings.HasSuffix(base, DeadLetterTopicSuffix) {
		return "", errors.New("base topic cannot already be a retry or dead-letter topic")
	}
	value := base + RetryTopicSuffix
	if err := ValidateTopic(value); err != nil {
		return "", err
	}
	return value, nil
}

func DeadLetterTopic(base string) (string, error) {
	if err := ValidateTopic(base); err != nil {
		return "", err
	}
	if strings.HasSuffix(base, RetryTopicSuffix) || strings.HasSuffix(base, DeadLetterTopicSuffix) {
		return "", errors.New("base topic cannot already be a retry or dead-letter topic")
	}
	value := base + DeadLetterTopicSuffix
	if err := ValidateTopic(value); err != nil {
		return "", err
	}
	return value, nil
}

func ValidateTopic(topic string) error {
	if topic == "" || len(topic) > 249 || topic == "." || topic == ".." {
		return errors.New("invalid Kafka topic length or reserved name")
	}
	for _, ch := range topic {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '_' || ch == '-') {
			return errors.New("Kafka topic must use lowercase ASCII letters, digits, dot, underscore, or hyphen")
		}
	}
	return nil
}

func PartitionKey(event eventbus.Event) ([]byte, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}
	hash := sha256.New()
	for _, part := range []string{event.OrganizationID, event.WorkspaceID, event.EntityType, event.EntityID} {
		hash.Write([]byte{0})
		hash.Write([]byte(part))
	}
	return hash.Sum(nil), nil
}

type Publisher struct{ producer Producer }

func NewPublisher(producer Producer) (*Publisher, error) {
	if err := validateProducer(producer); err != nil {
		return nil, err
	}
	return &Publisher{producer: producer}, nil
}

func (p *Publisher) Publish(ctx context.Context, event eventbus.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return fmt.Errorf("event validation failed: %w", err)
	}
	topic, err := TopicForEventType(event.Type)
	if err != nil {
		return err
	}
	encoded, err := event.MarshalJSON()
	if err != nil {
		return errors.New("event encoding failed")
	}
	key, err := PartitionKey(event)
	if err != nil {
		return errors.New("event partition key failed")
	}
	message := Message{
		Topic:     topic,
		Key:       key,
		Value:     encoded,
		Timestamp: event.OccurredAt.Time(),
		Headers:   baseHeaders(event),
	}
	if err := p.producer.Produce(ctx, message); err != nil {
		return errors.New("Kafka event publish failed")
	}
	return nil
}

func baseHeaders(event eventbus.Event) []Header {
	return []Header{
		{Key: HeaderContentType, Value: []byte(ContentTypeJSON)},
		{Key: HeaderEventID, Value: []byte(event.ID)},
		{Key: HeaderEventType, Value: []byte(event.Type.String())},
	}
}

type RetryPolicy struct {
	MaxAttempts    uint16
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

func (p RetryPolicy) Validate() error {
	if p.MaxAttempts == 0 || p.MaxAttempts > 100 {
		return errors.New("retry max attempts must be between 1 and 100")
	}
	if p.InitialBackoff <= 0 || p.MaxBackoff <= 0 || p.InitialBackoff > p.MaxBackoff {
		return errors.New("retry backoff bounds are invalid")
	}
	if p.MaxBackoff > 24*time.Hour {
		return errors.New("retry max backoff cannot exceed 24 hours")
	}
	return nil
}

func (p RetryPolicy) Backoff(failedAttempt uint16) time.Duration {
	value := p.InitialBackoff
	for i := uint16(1); i < failedAttempt && value < p.MaxBackoff; i++ {
		if value > p.MaxBackoff/2 {
			return p.MaxBackoff
		}
		value *= 2
	}
	if value > p.MaxBackoff {
		return p.MaxBackoff
	}
	return value
}

type Clock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }
func (realClock) Sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Consumer implements deterministic at-least-once retry/DLQ routing. It never
// commits the source record until the handler succeeds or the replacement
// retry/dead-letter record is durably acknowledged by the producer.
type Consumer struct {
	reader   Reader
	producer Producer
	policy   RetryPolicy
	clock    Clock
	base     map[string]struct{}
	topics   []string
}

const (
	consumerErrorInitialBackoff = 250 * time.Millisecond
	consumerErrorMaxBackoff     = 5 * time.Second
)

func NewConsumer(reader Reader, producer Producer, baseTopics []string, policy RetryPolicy, clock Clock) (*Consumer, error) {
	if reader == nil {
		return nil, errors.New("Kafka reader is required")
	}
	if err := validateProducer(producer); err != nil {
		return nil, err
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if len(baseTopics) == 0 {
		return nil, errors.New("at least one base topic is required")
	}
	if clock == nil {
		clock = realClock{}
	}
	base := make(map[string]struct{}, len(baseTopics))
	topics := make([]string, 0, len(baseTopics)*2)
	for _, topic := range baseTopics {
		if err := ValidateTopic(topic); err != nil {
			return nil, err
		}
		if strings.HasSuffix(topic, RetryTopicSuffix) || strings.HasSuffix(topic, DeadLetterTopicSuffix) {
			return nil, errors.New("consumer base topic cannot be retry or dead-letter topic")
		}
		if _, duplicate := base[topic]; duplicate {
			return nil, errors.New("duplicate consumer base topic")
		}
		base[topic] = struct{}{}
		retry, _ := RetryTopic(topic)
		topics = append(topics, topic, retry)
	}
	return &Consumer{reader: reader, producer: producer, policy: policy, clock: clock, base: base, topics: topics}, nil
}

func (c *Consumer) Topics() []string { return append([]string(nil), c.topics...) }

func (c *Consumer) Run(ctx context.Context, handler eventbus.Handler) error {
	if handler == nil {
		return errors.New("event handler is required")
	}
	backoff := consumerErrorInitialBackoff
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		message, err := c.reader.Read(ctx)
		if err != nil {
			return errors.New("Kafka read failed")
		}
		if err := c.HandleOne(ctx, message, handler); err != nil {
			// A commit or retry/DLQ publish can fail while Kafka is recovering.
			// Leave the source offset uncommitted and retry the read loop with
			// bounded backoff; franz-go will redeliver the same record once the
			// broker is available instead of taking the whole worker down.
			if err := c.clock.Sleep(ctx, backoff); err != nil {
				return err
			}
			if backoff < consumerErrorMaxBackoff {
				backoff *= 2
				if backoff > consumerErrorMaxBackoff {
					backoff = consumerErrorMaxBackoff
				}
			}
			continue
		}
		backoff = consumerErrorInitialBackoff
	}
}

func (c *Consumer) HandleOne(ctx context.Context, message Message, handler eventbus.Handler) error {
	if handler == nil {
		return errors.New("event handler is required")
	}
	baseTopic, retryRecord, ok := c.classifyTopic(message.Topic)
	if !ok {
		return errors.New("Kafka record arrived from an unconfigured topic")
	}

	metadata, err := c.deliveryMetadata(message, baseTopic, retryRecord)
	if err != nil {
		return c.deadLetterAndCommit(ctx, message, baseTopic, 1, c.nowInstant(), TerminalMetadata, TerminalMetadata)
	}
	if retryRecord {
		now := c.clock.Now().UTC()
		if metadata.NotBefore.After(now) {
			if err := c.clock.Sleep(ctx, metadata.NotBefore.Sub(now)); err != nil {
				return err
			}
		}
	}

	var event eventbus.Event
	if err := event.UnmarshalJSON(message.Value); err != nil {
		return c.deadLetterAndCommit(ctx, message, baseTopic, metadata.Attempt, metadata.FirstObserved, TerminalInvalidEvent, TerminalInvalidEvent)
	}
	headerEventID, _, _ := uniqueHeader(message.Headers, HeaderEventID)
	headerEventType, _, _ := uniqueHeader(message.Headers, HeaderEventType)
	if string(headerEventID) != event.ID || string(headerEventType) != event.Type.String() {
		return c.deadLetterAndCommit(ctx, message, baseTopic, metadata.Attempt, metadata.FirstObserved, TerminalHeaderMismatch, TerminalHeaderMismatch)
	}
	expectedTopic, err := TopicForEventType(event.Type)
	if err != nil || expectedTopic != baseTopic {
		return c.deadLetterAndCommit(ctx, message, baseTopic, metadata.Attempt, metadata.FirstObserved, TerminalTopicMismatch, TerminalTopicMismatch)
	}

	delivery := eventbus.Delivery{Event: event, Attempt: metadata.Attempt, FirstObservedAt: metadata.FirstObserved}
	if err := delivery.Validate(); err != nil {
		return c.deadLetterAndCommit(ctx, message, baseTopic, metadata.Attempt, metadata.FirstObserved, TerminalInvalidEvent, TerminalInvalidEvent)
	}
	handlerErr := handler(ctx, delivery)
	if handlerErr == nil {
		return c.commit(ctx, message)
	}
	class, code := eventbus.ClassifyFailure(handlerErr)
	if class == eventbus.FailurePermanent {
		return c.deadLetterAndCommit(ctx, message, baseTopic, metadata.Attempt, metadata.FirstObserved, code, TerminalPermanent)
	}
	if metadata.Attempt >= c.policy.MaxAttempts {
		return c.deadLetterAndCommit(ctx, message, baseTopic, metadata.Attempt, metadata.FirstObserved, code, TerminalRetryExhausted)
	}
	return c.retryAndCommit(ctx, message, event, baseTopic, metadata, code)
}

type deliveryMetadata struct {
	Attempt       uint16
	FirstObserved domain.UTCInstant
	NotBefore     time.Time
}

func (c *Consumer) deliveryMetadata(message Message, baseTopic string, retry bool) (deliveryMetadata, error) {
	if err := validateSystemHeaders(message.Headers); err != nil {
		return deliveryMetadata{}, err
	}
	contentType, ok, err := uniqueHeader(message.Headers, HeaderContentType)
	if err != nil || !ok || string(contentType) != ContentTypeJSON {
		return deliveryMetadata{}, errors.New("Kafka record content type is invalid")
	}
	if _, ok, err := uniqueHeader(message.Headers, HeaderEventID); err != nil || !ok {
		return deliveryMetadata{}, errors.New("Kafka record has no event id header")
	}
	if _, ok, err := uniqueHeader(message.Headers, HeaderEventType); err != nil || !ok {
		return deliveryMetadata{}, errors.New("Kafka record has no event type header")
	}
	if !retry {
		for _, key := range []string{HeaderAttempt, HeaderFirstObservedAt, HeaderOriginalTopic, HeaderNotBefore, HeaderFailureCode, HeaderTerminalReason} {
			if _, ok, _ := uniqueHeader(message.Headers, key); ok {
				return deliveryMetadata{}, errors.New("base record contains retry metadata")
			}
		}
		return deliveryMetadata{Attempt: 1, FirstObserved: c.nowInstant()}, nil
	}
	original, ok, err := uniqueHeader(message.Headers, HeaderOriginalTopic)
	if err != nil || !ok || string(original) != baseTopic {
		return deliveryMetadata{}, errors.New("retry record has invalid original topic")
	}
	attemptRaw, ok, err := uniqueHeader(message.Headers, HeaderAttempt)
	if err != nil || !ok {
		return deliveryMetadata{}, errors.New("retry record has no attempt")
	}
	attempt64, err := strconv.ParseUint(string(attemptRaw), 10, 16)
	if err != nil || attempt64 < 2 || attempt64 > 1000 {
		return deliveryMetadata{}, errors.New("retry record attempt is invalid")
	}
	firstRaw, ok, err := uniqueHeader(message.Headers, HeaderFirstObservedAt)
	if err != nil || !ok {
		return deliveryMetadata{}, errors.New("retry record has no first observed timestamp")
	}
	first, err := domain.ParseUTCInstant(string(firstRaw))
	if err != nil {
		return deliveryMetadata{}, errors.New("retry record first observed timestamp is invalid")
	}
	notBeforeRaw, ok, err := uniqueHeader(message.Headers, HeaderNotBefore)
	if err != nil || !ok {
		return deliveryMetadata{}, errors.New("retry record has no not-before timestamp")
	}
	notBefore, err := domain.ParseUTCInstant(string(notBeforeRaw))
	if err != nil {
		return deliveryMetadata{}, errors.New("retry record not-before timestamp is invalid")
	}
	return deliveryMetadata{Attempt: uint16(attempt64), FirstObserved: first, NotBefore: notBefore.Time()}, nil
}

func (c *Consumer) retryAndCommit(ctx context.Context, source Message, event eventbus.Event, baseTopic string, metadata deliveryMetadata, failureCode string) error {
	retryTopic, _ := RetryTopic(baseTopic)
	nextAttempt := metadata.Attempt + 1
	notBeforeTime := c.clock.Now().UTC().Add(c.policy.Backoff(metadata.Attempt))
	notBefore, err := domain.NewUTCInstant(notBeforeTime)
	if err != nil {
		return errors.New("retry timestamp creation failed")
	}
	key, err := PartitionKey(event)
	if err != nil {
		return errors.New("retry partition key creation failed")
	}
	retry := Message{
		Topic:     retryTopic,
		Key:       key,
		Value:     append([]byte(nil), source.Value...),
		Timestamp: event.OccurredAt.Time(),
		Headers: append(baseHeaders(event),
			Header{Key: HeaderAttempt, Value: []byte(strconv.FormatUint(uint64(nextAttempt), 10))},
			Header{Key: HeaderFirstObservedAt, Value: []byte(metadata.FirstObserved.String())},
			Header{Key: HeaderOriginalTopic, Value: []byte(baseTopic)},
			Header{Key: HeaderNotBefore, Value: []byte(notBefore.String())},
			Header{Key: HeaderFailureCode, Value: []byte(failureCode)},
		),
	}
	if err := c.producer.Produce(ctx, retry); err != nil {
		return errors.New("Kafka retry publish failed")
	}
	return c.commit(ctx, source)
}

func (c *Consumer) deadLetterAndCommit(ctx context.Context, source Message, baseTopic string, attempt uint16, first domain.UTCInstant, failureCode, terminalReason string) error {
	dlqTopic, _ := DeadLetterTopic(baseTopic)
	if first.Validate() != nil {
		first = c.nowInstant()
	}
	headers := []Header{
		{Key: HeaderContentType, Value: []byte(ContentTypeJSON)},
		{Key: HeaderAttempt, Value: []byte(strconv.FormatUint(uint64(attempt), 10))},
		{Key: HeaderFirstObservedAt, Value: []byte(first.String())},
		{Key: HeaderOriginalTopic, Value: []byte(baseTopic)},
		{Key: HeaderFailureCode, Value: []byte(failureCode)},
		{Key: HeaderTerminalReason, Value: []byte(terminalReason)},
	}
	if id, ok, _ := uniqueHeader(source.Headers, HeaderEventID); ok {
		headers = append(headers, Header{Key: HeaderEventID, Value: append([]byte(nil), id...)})
	}
	if typ, ok, _ := uniqueHeader(source.Headers, HeaderEventType); ok {
		headers = append(headers, Header{Key: HeaderEventType, Value: append([]byte(nil), typ...)})
	}
	dlq := Message{Topic: dlqTopic, Key: append([]byte(nil), source.Key...), Value: append([]byte(nil), source.Value...), Timestamp: source.Timestamp, Headers: headers}
	if err := c.producer.Produce(ctx, dlq); err != nil {
		return errors.New("Kafka dead-letter publish failed")
	}
	return c.commit(ctx, source)
}

func (c *Consumer) commit(ctx context.Context, message Message) error {
	if err := c.reader.Commit(ctx, message); err != nil {
		return errors.New("Kafka offset commit failed")
	}
	return nil
}

func (c *Consumer) classifyTopic(topic string) (string, bool, bool) {
	if _, ok := c.base[topic]; ok {
		return topic, false, true
	}
	if strings.HasSuffix(topic, RetryTopicSuffix) {
		base := strings.TrimSuffix(topic, RetryTopicSuffix)
		_, ok := c.base[base]
		return base, true, ok
	}
	return "", false, false
}

func (c *Consumer) nowInstant() domain.UTCInstant {
	value, err := domain.NewUTCInstant(c.clock.Now().UTC())
	if err != nil {
		panic("eventbus clock returned zero instant")
	}
	return value
}

func validateSystemHeaders(headers []Header) error {
	total := 0
	seen := make(map[string]struct{})
	for _, header := range headers {
		if header.Key == "" || len(header.Key) > 128 || len(header.Value) > maxHeaderValueBytes {
			return errors.New("Kafka header is invalid")
		}
		normalized := strings.ToLower(header.Key)
		if strings.HasPrefix(normalized, "x-torgnexa-") || normalized == HeaderContentType {
			if _, duplicate := seen[normalized]; duplicate {
				return errors.New("Kafka system header is duplicated")
			}
			seen[normalized] = struct{}{}
			total += len(header.Key) + len(header.Value)
			if total > maxSystemHeaderBytes {
				return errors.New("Kafka system headers exceed maximum size")
			}
		}
	}
	return nil
}

func uniqueHeader(headers []Header, key string) ([]byte, bool, error) {
	var value []byte
	found := false
	for _, header := range headers {
		if !strings.EqualFold(header.Key, key) {
			continue
		}
		if found {
			return nil, false, errors.New("Kafka system header is duplicated")
		}
		value = header.Value
		found = true
	}
	return value, found, nil
}
