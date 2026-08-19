// Package siem implements asynchronous minimized security-event export.
package siem

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"strings"
	"sync"
	"time"
)

var ErrInvalid = errors.New("siem: invalid value")

type Severity string

const (
	Info     Severity = "info"
	Warning  Severity = "warning"
	Critical Severity = "critical"
)

type SecurityEvent struct {
	ID, Type, ActorFingerprint, ResourceType, ResourceID, Outcome, CorrelationID string
	Severity                                                                     Severity
	OccurredAt                                                                   time.Time
	Attributes                                                                   map[string]string
}

func (e SecurityEvent) Validate() error {
	if e.ID == "" || e.Type == "" || e.ActorFingerprint == "" || e.Outcome == "" || e.OccurredAt.IsZero() || e.OccurredAt.Location() != time.UTC {
		return ErrInvalid
	}
	if e.Severity != Info && e.Severity != Warning && e.Severity != Critical {
		return ErrInvalid
	}
	for k, v := range e.Attributes {
		if len(k) > 64 || len(v) > 256 || secrets.SensitiveString(v) {
			return ErrInvalid
		}
	}
	return nil
}

type SinkKind string

const (
	SyslogTLS     SinkKind = "syslog_tls"
	SignedWebhook SinkKind = "signed_webhook"
	Kafka         SinkKind = "kafka"
	OTLP          SinkKind = "otlp"
)

type Sink interface {
	ID() string
	Kind() SinkKind
	Export(context.Context, tenancy.Scope, SecurityEvent) error
}
type Item struct {
	Scope         tenancy.Scope
	Event         SecurityEvent
	Attempts      int
	NextAttemptAt time.Time
	LastErrorCode string
}
type Queue struct {
	mu    sync.Mutex
	items []Item
	dlq   []Item
	done  map[string]bool
}

func NewQueue() *Queue { return &Queue{done: map[string]bool{}} }
func (q *Queue) Enqueue(scope tenancy.Scope, e SecurityEvent) error {
	if !scope.Valid() || e.Validate() != nil {
		return ErrInvalid
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	dedupeKey := scope.OrganizationID().String() + "/" + scope.WorkspaceID().String() + "/" + e.ID
	if q.done[dedupeKey] {
		return nil
	}
	for _, i := range q.items {
		if i.Scope == scope && i.Event.ID == e.ID {
			return nil
		}
	}
	q.items = append(q.items, Item{Scope: scope, Event: e})
	return nil
}
func (q *Queue) Pending() int { q.mu.Lock(); defer q.mu.Unlock(); return len(q.items) }
func (q *Queue) DLQ() int     { q.mu.Lock(); defer q.mu.Unlock(); return len(q.dlq) }

type Worker struct {
	Queue       *Queue
	Sinks       []Sink
	MaxAttempts int
	Now         func() time.Time
}

func (w *Worker) RunOne(ctx context.Context) error {
	if w == nil || w.Queue == nil || len(w.Sinks) == 0 {
		return ErrInvalid
	}
	if w.MaxAttempts < 1 {
		w.MaxAttempts = 5
	}
	if w.Now == nil {
		w.Now = time.Now
	}
	w.Queue.mu.Lock()
	if len(w.Queue.items) == 0 {
		w.Queue.mu.Unlock()
		return nil
	}
	it := w.Queue.items[0]
	w.Queue.mu.Unlock()
	for _, s := range w.Sinks {
		if s == nil {
			return ErrInvalid
		}
		if err := s.Export(ctx, it.Scope, it.Event); err != nil {
			w.Queue.mu.Lock()
			defer w.Queue.mu.Unlock()
			w.Queue.items[0].Attempts++
			w.Queue.items[0].LastErrorCode = "sink_failed"
			if w.Queue.items[0].Attempts >= w.MaxAttempts {
				w.Queue.dlq = append(w.Queue.dlq, w.Queue.items[0])
				w.Queue.items = w.Queue.items[1:]
			} else {
				w.Queue.items[0].NextAttemptAt = w.Now().UTC().Add(time.Second * time.Duration(1<<min(w.Queue.items[0].Attempts, 6)))
			}
			return err
		}
	}
	w.Queue.mu.Lock()
	dedupeKey := it.Scope.OrganizationID().String() + "/" + it.Scope.WorkspaceID().String() + "/" + it.Event.ID
	w.Queue.done[dedupeKey] = true
	w.Queue.items = w.Queue.items[1:]
	w.Queue.mu.Unlock()
	return nil
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func RFC5424(e SecurityEvent) (string, error) {
	if e.Validate() != nil {
		return "", ErrInvalid
	}
	b, _ := json.Marshal(map[string]any{"id": e.ID, "type": e.Type, "severity": e.Severity, "actor_fp": e.ActorFingerprint, "resource_type": e.ResourceType, "resource_id": e.ResourceID, "outcome": e.Outcome, "correlation_id": e.CorrelationID, "attributes": e.Attributes})
	return fmt.Sprintf("<134>1 %s torgnexa security - - - %s", e.OccurredAt.Format(time.RFC3339), string(b)), nil
}
func SignWebhook(secret []byte, e SecurityEvent) (string, []byte, error) {
	if len(secret) < 16 || e.Validate() != nil {
		return "", nil, ErrInvalid
	}
	body, err := json.Marshal(e)
	if err != nil {
		return "", nil, err
	}
	m := hmac.New(sha256.New, secret)
	_, _ = m.Write(body)
	return hex.EncodeToString(m.Sum(nil)), body, nil
}
func OTLPAttributes(e SecurityEvent) (map[string]string, error) {
	if e.Validate() != nil {
		return nil, ErrInvalid
	}
	m := map[string]string{"event.id": e.ID, "event.name": e.Type, "event.outcome": e.Outcome, "security.severity": string(e.Severity), "actor.fingerprint": e.ActorFingerprint}
	for k, v := range e.Attributes {
		m["torgnexa."+strings.ToLower(k)] = v
	}
	return m, nil
}

// ByteSender is a host-owned TLS/network boundary used by concrete SIEM sinks.
// Implementations are responsible for endpoint allowlisting, TLS policy and timeouts.
type ByteSender interface {
	Send(context.Context, string, []byte, map[string]string) error
}

type SyslogTLSSink struct {
	SinkID, Endpoint string
	Sender           ByteSender
}

func (s SyslogTLSSink) ID() string     { return s.SinkID }
func (s SyslogTLSSink) Kind() SinkKind { return SyslogTLS }
func (s SyslogTLSSink) Export(ctx context.Context, _ tenancy.Scope, e SecurityEvent) error {
	if s.SinkID == "" || s.Endpoint == "" || s.Sender == nil {
		return ErrInvalid
	}
	msg, err := RFC5424(e)
	if err != nil {
		return err
	}
	return s.Sender.Send(ctx, s.Endpoint, []byte(msg), map[string]string{"Content-Type": "application/syslog"})
}

type WebhookEncoder interface {
	Encode(context.Context, tenancy.Scope, SecurityEvent) (map[string]string, []byte, error)
}
type SignedWebhookSinkAdapter struct {
	SinkID, Endpoint string
	Sender           ByteSender
	Encoder          WebhookEncoder
}

func (s SignedWebhookSinkAdapter) ID() string     { return s.SinkID }
func (s SignedWebhookSinkAdapter) Kind() SinkKind { return SignedWebhook }
func (s SignedWebhookSinkAdapter) Export(ctx context.Context, scope tenancy.Scope, e SecurityEvent) error {
	if s.SinkID == "" || s.Endpoint == "" || s.Sender == nil || s.Encoder == nil {
		return ErrInvalid
	}
	headers, body, err := s.Encoder.Encode(ctx, scope, e)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return ErrInvalid
	}
	return s.Sender.Send(ctx, s.Endpoint, body, headers)
}

type MessageProducer interface {
	Produce(context.Context, string, string, []byte) error
}
type KafkaSinkAdapter struct {
	SinkID, Topic string
	Producer      MessageProducer
}

func (s KafkaSinkAdapter) ID() string     { return s.SinkID }
func (s KafkaSinkAdapter) Kind() SinkKind { return Kafka }
func (s KafkaSinkAdapter) Export(ctx context.Context, scope tenancy.Scope, e SecurityEvent) error {
	if s.SinkID == "" || s.Topic == "" || s.Producer == nil || e.Validate() != nil || !scope.Valid() {
		return ErrInvalid
	}
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return s.Producer.Produce(ctx, s.Topic, scope.OrganizationID().String()+"/"+e.ID, body)
}

type SecurityLogExporter interface {
	ExportSecurityLog(context.Context, tenancy.Scope, map[string]string, time.Time) error
}
type OTLPSinkAdapter struct {
	SinkID   string
	Exporter SecurityLogExporter
}

func (s OTLPSinkAdapter) ID() string     { return s.SinkID }
func (s OTLPSinkAdapter) Kind() SinkKind { return OTLP }
func (s OTLPSinkAdapter) Export(ctx context.Context, scope tenancy.Scope, e SecurityEvent) error {
	if s.SinkID == "" || s.Exporter == nil || !scope.Valid() {
		return ErrInvalid
	}
	attrs, err := OTLPAttributes(e)
	if err != nil {
		return err
	}
	return s.Exporter.ExportSecurityLog(ctx, scope, attrs, e.OccurredAt)
}

type QueueMetrics struct {
	Pending, DLQ     int
	OldestPendingLag time.Duration
}

func (q *Queue) Metrics(now time.Time) QueueMetrics {
	if q == nil {
		return QueueMetrics{}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	m := QueueMetrics{Pending: len(q.items), DLQ: len(q.dlq)}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	for _, item := range q.items {
		lag := now.UTC().Sub(item.Event.OccurredAt)
		if lag > m.OldestPendingLag {
			m.OldestPendingLag = lag
		}
	}
	return m
}
