// Package kafkatransport adapts franz-go to the narrow kafkaeventbus transport
// interfaces. Kafka client types never cross this package boundary.
package kafkatransport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/torgnexa/torgnexa/internal/platform/kafkaeventbus"
)

const maxRecordBytes = 16 << 20

const (
	readerInitialBackoff = 250 * time.Millisecond
	readerMaxBackoff     = 5 * time.Second
)

type Producer struct{ client *kgo.Client }

func NewProducer(brokers []string, clientID string) (*Producer, error) {
	if len(brokers) == 0 || clientID == "" {
		return nil, errors.New("kafka transport: producer configuration required")
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID(clientID),
		kgo.ProducerBatchMaxBytes(maxRecordBytes),
		kgo.MaxBufferedBytes(64<<20),
		kgo.RecordDeliveryTimeout(30*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("kafka transport: producer: %w", err)
	}
	return &Producer{client: client}, nil
}

func (p *Producer) Capabilities() kafkaeventbus.ProducerCapabilities {
	// franz-go enables idempotent production and all-ISR acknowledgements by
	// default. We deliberately never opt out of either behavior here.
	return kafkaeventbus.ProducerCapabilities{Idempotent: true, AcksAll: true}
}

func (p *Producer) Produce(ctx context.Context, message kafkaeventbus.Message) error {
	if ctx == nil || p == nil || p.client == nil || kafkaeventbus.ValidateTopic(message.Topic) != nil || len(message.Value) > maxRecordBytes {
		return errors.New("kafka transport: invalid produce")
	}
	headers := make([]kgo.RecordHeader, 0, len(message.Headers))
	for _, h := range message.Headers {
		headers = append(headers, kgo.RecordHeader{Key: h.Key, Value: append([]byte(nil), h.Value...)})
	}
	record := &kgo.Record{Topic: message.Topic, Key: append([]byte(nil), message.Key...), Value: append([]byte(nil), message.Value...), Headers: headers, Timestamp: message.Timestamp}
	if err := p.client.ProduceSync(ctx, record).FirstErr(); err != nil {
		return fmt.Errorf("kafka transport: produce: %w", err)
	}
	return nil
}

func (p *Producer) Close() error {
	if p == nil || p.client == nil {
		return nil
	}
	p.client.Close()
	return nil
}

type Reader struct{ client *kgo.Client }

func NewReader(brokers []string, clientID, group string, topics []string) (*Reader, error) {
	if len(brokers) == 0 || clientID == "" || group == "" || len(topics) == 0 {
		return nil, errors.New("kafka transport: reader configuration required")
	}
	for _, topic := range topics {
		if kafkaeventbus.ValidateTopic(topic) != nil {
			return nil, errors.New("kafka transport: invalid topic")
		}
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID(clientID),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topics...),
		kgo.DisableAutoCommit(),
		kgo.FetchMaxBytes(32<<20),
		kgo.FetchMaxPartitionBytes(maxRecordBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("kafka transport: reader: %w", err)
	}
	return &Reader{client: client}, nil
}

func (r *Reader) Read(ctx context.Context) (kafkaeventbus.Message, error) {
	if ctx == nil || r == nil || r.client == nil {
		return kafkaeventbus.Message{}, errors.New("kafka transport: invalid read")
	}
	backoff := readerInitialBackoff
	for {
		fetches := r.client.PollRecords(ctx, 1)
		records := fetches.Records()
		if len(records) > 0 {
			record := records[0]
			headers := make([]kafkaeventbus.Header, 0, len(record.Headers))
			for _, h := range record.Headers {
				headers = append(headers, kafkaeventbus.Header{Key: h.Key, Value: append([]byte(nil), h.Value...)})
			}
			return kafkaeventbus.Message{Topic: record.Topic, Key: append([]byte(nil), record.Key...), Value: append([]byte(nil), record.Value...), Headers: headers, Timestamp: record.Timestamp.UTC(), Opaque: record}, nil
		}
		if err := ctx.Err(); err != nil {
			return kafkaeventbus.Message{}, err
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			// franz-go reports broker disconnects and leader/group-coordinator
			// transitions as fetch errors. Keep the reader alive with bounded
			// exponential backoff so a single-node broker restart does not make
			// the worker abandon all consumers and rely on a process restart.
			if err := sleepContext(ctx, backoff); err != nil {
				return kafkaeventbus.Message{}, err
			}
			if backoff < readerMaxBackoff {
				backoff *= 2
				if backoff > readerMaxBackoff {
					backoff = readerMaxBackoff
				}
			}
			continue
		}
		backoff = readerInitialBackoff
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *Reader) Commit(ctx context.Context, message kafkaeventbus.Message) error {
	if ctx == nil || r == nil || r.client == nil {
		return errors.New("kafka transport: invalid commit")
	}
	record, ok := message.Opaque.(*kgo.Record)
	if !ok || record == nil {
		return errors.New("kafka transport: foreign record")
	}
	if err := r.client.CommitRecords(ctx, record); err != nil {
		return fmt.Errorf("kafka transport: commit: %w", err)
	}
	return nil
}

func (r *Reader) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	r.client.Close()
	return nil
}

var _ kafkaeventbus.Producer = (*Producer)(nil)
var _ kafkaeventbus.Reader = (*Reader)(nil)
