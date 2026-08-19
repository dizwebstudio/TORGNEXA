package outbox

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
	testOrg = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	testWS  = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
)

func TestRelayPublishesAndAcknowledgesLease(t *testing.T) {
	t.Parallel()
	event := validEvent(t)
	repo := &fakeRepository{claimed: []Claimed{claimedEvent(t, event, 1)}}
	publisher := &fakePublisher{}
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	relay, err := newRelay(repo, publisher, "relay-1", DefaultRetryPolicy(), fixedClock{now: now})
	if err != nil {
		t.Fatal(err)
	}
	count, err := relay.RunOnce(context.Background(), mustScope(t))
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if count != 1 || len(publisher.events) != 1 || len(repo.published) != 1 || len(repo.retries) != 0 {
		t.Fatalf("count=%d publishes=%d acks=%d retries=%d", count, len(publisher.events), len(repo.published), len(repo.retries))
	}
	if repo.published[0].eventID != event.ID || repo.published[0].token != "00112233445566778899aabbccddeeff" || !repo.published[0].at.Equal(now) {
		t.Fatalf("published metadata = %#v", repo.published[0])
	}
}

func TestRelaySchedulesBoundedRetryWithoutPersistingBrokerError(t *testing.T) {
	t.Parallel()
	event := validEvent(t)
	repo := &fakeRepository{claimed: []Claimed{claimedEvent(t, event, 4)}}
	publisher := &fakePublisher{err: errors.New("Bearer super-secret broker response")}
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	policy := RetryPolicy{BatchSize: 10, LeaseDuration: 30 * time.Second, BaseDelay: time.Second, MaxDelay: 4 * time.Second}
	relay, _ := newRelay(repo, publisher, "relay-1", policy, fixedClock{now: now})
	if _, err := relay.RunOnce(context.Background(), mustScope(t)); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(repo.retries) != 1 {
		t.Fatalf("retries=%d", len(repo.retries))
	}
	retry := repo.retries[0]
	if retry.code != "publish_failed" || !retry.at.Equal(now.Add(4*time.Second)) {
		t.Fatalf("retry = %#v", retry)
	}
	if len(repo.published) != 0 {
		t.Fatal("failed publish was acknowledged")
	}
}

func TestRelayLeavesLeaseForRecoveryOnCancellation(t *testing.T) {
	t.Parallel()
	event := validEvent(t)
	repo := &fakeRepository{claimed: []Claimed{claimedEvent(t, event, 1)}}
	ctx, cancel := context.WithCancel(context.Background())
	publisher := publisherFunc(func(context.Context, eventbus.Event) error { cancel(); return context.Canceled })
	relay, _ := newRelay(repo, publisher, "relay-1", DefaultRetryPolicy(), fixedClock{now: time.Now().UTC()})
	if _, err := relay.RunOnce(ctx, mustScope(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(repo.published) != 0 || len(repo.retries) != 0 {
		t.Fatal("canceled publication mutated outbox metadata")
	}
}

func TestRelayRejectsCrossTenantClaim(t *testing.T) {
	t.Parallel()
	event := validEvent(t)
	event.WorkspaceID = "018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002"
	repo := &fakeRepository{claimed: []Claimed{claimedEvent(t, event, 1)}}
	relay, _ := newRelay(repo, &fakePublisher{}, "relay-1", DefaultRetryPolicy(), fixedClock{now: time.Now().UTC()})
	if _, err := relay.RunOnce(context.Background(), mustScope(t)); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("RunOnce() error = %v", err)
	}
}

func TestRetryPolicyValidationAndBackoff(t *testing.T) {
	t.Parallel()
	if _, err := NewRelay(&fakeRepository{}, &fakePublisher{}, "bad worker!", DefaultRetryPolicy()); err == nil {
		t.Fatal("invalid worker accepted")
	}
	bad := DefaultRetryPolicy()
	bad.BatchSize = 0
	if _, err := NewRelay(&fakeRepository{}, &fakePublisher{}, "worker", bad); err == nil {
		t.Fatal("invalid policy accepted")
	}
	relay, _ := newRelay(&fakeRepository{}, &fakePublisher{}, "worker", RetryPolicy{BatchSize: 1, LeaseDuration: time.Second, BaseDelay: time.Second, MaxDelay: 5 * time.Second}, fixedClock{now: time.Now().UTC()})
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
	for i, expected := range want {
		if got := relay.retryDelay(uint32(i + 1)); got != expected {
			t.Fatalf("attempt %d delay=%v want=%v", i+1, got, expected)
		}
	}
}

func validEvent(t *testing.T) eventbus.Event {
	t.Helper()
	instant, _ := domain.NewUTCInstant(time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC))
	typ, _ := eventbus.ParseEventType("commerce.orders.order_created.v1")
	return eventbus.Event{ID: "evt_outbox_001", Type: typ, OccurredAt: instant, OrganizationID: testOrg, WorkspaceID: testWS, EntityType: "order", EntityID: "order_001", Source: "orders", CorrelationID: "corr_001", Data: json.RawMessage(`{"order_id":"order_001"}`)}
}

func claimedEvent(t *testing.T, event eventbus.Event, attempt uint32) Claimed {
	t.Helper()
	until, _ := domain.NewUTCInstant(time.Date(2026, 8, 9, 10, 1, 0, 0, time.UTC))
	return Claimed{Event: event, Attempt: attempt, LeaseToken: "00112233445566778899aabbccddeeff", LeaseOwner: "relay-1", LeaseUntil: until}
}

func mustScope(t *testing.T) tenancy.Scope {
	t.Helper()
	scope, err := tenancy.ParseScope(testOrg, testWS)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

type publisherFunc func(context.Context, eventbus.Event) error

func (fn publisherFunc) Publish(ctx context.Context, event eventbus.Event) error {
	return fn(ctx, event)
}

type fakePublisher struct {
	events []eventbus.Event
	err    error
}

func (publisher *fakePublisher) Publish(_ context.Context, event eventbus.Event) error {
	publisher.events = append(publisher.events, event)
	return publisher.err
}

type publishCall struct {
	eventID, token string
	at             time.Time
}
type retryCall struct {
	eventID, token, code string
	at                   time.Time
}
type fakeRepository struct {
	claimed    []Claimed
	claimErr   error
	published  []publishCall
	publishErr error
	retries    []retryCall
	retryErr   error
}

func (repository *fakeRepository) Claim(_ context.Context, _ tenancy.Scope, _ string, _ int, _ time.Duration) ([]Claimed, error) {
	return append([]Claimed(nil), repository.claimed...), repository.claimErr
}
func (repository *fakeRepository) MarkPublished(_ context.Context, _ tenancy.Scope, eventID, token string, at domain.UTCInstant) error {
	repository.published = append(repository.published, publishCall{eventID: eventID, token: token, at: at.Time()})
	return repository.publishErr
}
func (repository *fakeRepository) ReleaseForRetry(_ context.Context, _ tenancy.Scope, eventID, token string, at domain.UTCInstant, code string) error {
	repository.retries = append(repository.retries, retryCall{eventID: eventID, token: token, at: at.Time(), code: code})
	return repository.retryErr
}
