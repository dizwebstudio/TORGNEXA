package siem

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"strings"
	"testing"
	"time"
)

func scope(t *testing.T) tenancy.Scope {
	s, e := tenancy.ParseScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func ev() SecurityEvent {
	return SecurityEvent{ID: "evt_1", Type: "iam.role_mapping_changed", ActorFingerprint: strings.Repeat("a", 64), ResourceType: "iam_mapping", ResourceID: "m1", Outcome: "success", Severity: Critical, OccurredAt: time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC), Attributes: map[string]string{"change": "reviewed"}}
}

type sink struct {
	err   error
	calls int
}

func (s *sink) ID() string                                                 { return "s1" }
func (s *sink) Kind() SinkKind                                             { return SyslogTLS }
func (s *sink) Export(context.Context, tenancy.Scope, SecurityEvent) error { s.calls++; return s.err }
func TestSIEMOutageDoesNotLoseQueuedBusinessEvidence(t *testing.T) {
	q := NewQueue()
	if e := q.Enqueue(scope(t), ev()); e != nil {
		t.Fatal(e)
	}
	s := &sink{err: errors.New("down")}
	w := &Worker{Queue: q, Sinks: []Sink{s}, MaxAttempts: 2, Now: func() time.Time { return time.Now().UTC() }}
	if e := w.RunOne(context.Background()); e == nil {
		t.Fatal("want failure")
	}
	if q.Pending() != 1 {
		t.Fatal("event lost")
	}
	_ = w.RunOne(context.Background())
	if q.Pending() != 0 || q.DLQ() != 1 {
		t.Fatalf("pending=%d dlq=%d", q.Pending(), q.DLQ())
	}
}
func TestSecretsRejectedAndEncodersMinimized(t *testing.T) {
	e := ev()
	e.Attributes["bad"] = "Bearer abcdefghijklmnopqrstuvwxyz"
	if e.Validate() == nil {
		t.Fatal("secret accepted")
	}
	e = ev()
	line, err := RFC5424(e)
	if err != nil || strings.Contains(line, "Bearer ") {
		t.Fatalf("%q %v", line, err)
	}
	sig, body, err := SignWebhook([]byte("0123456789abcdef"), e)
	if err != nil || len(sig) != 64 || len(body) == 0 {
		t.Fatal("sign failed")
	}
}
func TestSuccessfulReplayIsIdempotentAndMetricsExposeLag(t *testing.T) {
	q := NewQueue()
	e := ev()
	if err := q.Enqueue(scope(t), e); err != nil {
		t.Fatal(err)
	}
	s := &sink{}
	w := &Worker{Queue: q, Sinks: []Sink{s}, Now: func() time.Time { return e.OccurredAt.Add(2 * time.Minute) }}
	m := q.Metrics(e.OccurredAt.Add(2 * time.Minute))
	if m.Pending != 1 || m.OldestPendingLag != 2*time.Minute {
		t.Fatalf("metrics=%+v", m)
	}
	if err := w.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(scope(t), e); err != nil {
		t.Fatal(err)
	}
	if q.Pending() != 0 || s.calls != 1 {
		t.Fatalf("pending=%d calls=%d", q.Pending(), s.calls)
	}
}
