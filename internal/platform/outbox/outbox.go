// Package outbox defines the transactional event intent and relay boundary.
package outbox

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

var (
	ErrInvalidRecord  = errors.New("outbox: invalid record")
	ErrLeaseLost      = errors.New("outbox: lease lost")
	ErrLegacyRows     = errors.New("outbox: legacy unpublished rows require migration")
	leaseTokenPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	workerPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
)

// Enqueuer is intentionally tiny so a PostgreSQL implementation can bind it to
// the caller's already-open domain transaction. Enqueue must never commit that
// transaction itself.
type Enqueuer interface {
	Enqueue(context.Context, eventbus.Event) error
}

// Claimed is an unpublished immutable event currently leased by one relay.
type Claimed struct {
	Event      eventbus.Event
	Attempt    uint32
	LeaseToken string
	LeaseOwner string
	LeaseUntil domain.UTCInstant
}

func (claimed Claimed) Validate() error {
	if err := claimed.Event.Validate(); err != nil {
		return fmt.Errorf("%w: event: %v", ErrInvalidRecord, err)
	}
	if claimed.Attempt == 0 || !leaseTokenPattern.MatchString(claimed.LeaseToken) || !workerPattern.MatchString(claimed.LeaseOwner) {
		return ErrInvalidRecord
	}
	if err := claimed.LeaseUntil.Validate(); err != nil {
		return ErrInvalidRecord
	}
	return nil
}

// Repository owns short-lived claim/ack/retry transactions. Implementations
// must scope every operation by organization/workspace and use compare-by-lease
// updates so stale workers cannot acknowledge a re-leased row.
type Repository interface {
	Claim(context.Context, tenancy.Scope, string, int, time.Duration) ([]Claimed, error)
	MarkPublished(context.Context, tenancy.Scope, string, string, domain.UTCInstant) error
	ReleaseForRetry(context.Context, tenancy.Scope, string, string, domain.UTCInstant, string) error
}

// RetryPolicy bounds relay pressure while retaining events indefinitely. There
// is deliberately no max-attempt discard: PostgreSQL outbox is authoritative
// committed intent and must not silently lose an event because Kafka is down.
type RetryPolicy struct {
	BatchSize     int
	LeaseDuration time.Duration
	BaseDelay     time.Duration
	MaxDelay      time.Duration
}

func (policy RetryPolicy) Validate() error {
	if policy.BatchSize < 1 || policy.BatchSize > 1000 {
		return errors.New("outbox retry policy: batch size must be between 1 and 1000")
	}
	if policy.LeaseDuration < time.Second || policy.LeaseDuration > 10*time.Minute {
		return errors.New("outbox retry policy: lease duration must be between 1s and 10m")
	}
	if policy.BaseDelay < 100*time.Millisecond || policy.BaseDelay > time.Hour {
		return errors.New("outbox retry policy: base delay must be between 100ms and 1h")
	}
	if policy.MaxDelay < policy.BaseDelay || policy.MaxDelay > 24*time.Hour {
		return errors.New("outbox retry policy: max delay must be between base delay and 24h")
	}
	return nil
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{BatchSize: 100, LeaseDuration: 30 * time.Second, BaseDelay: time.Second, MaxDelay: 5 * time.Minute}
}

type clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// Relay publishes claimed rows to EventBus. Publish happens outside the claim
// transaction. A crash after publish but before MarkPublished can duplicate an
// event with the same immutable Event.ID; Task 009 consumers must deduplicate.
type Relay struct {
	repository Repository
	publisher  eventbus.Publisher
	workerID   string
	policy     RetryPolicy
	clock      clock
}

func NewRelay(repository Repository, publisher eventbus.Publisher, workerID string, policy RetryPolicy) (*Relay, error) {
	return newRelay(repository, publisher, workerID, policy, systemClock{})
}

func newRelay(repository Repository, publisher eventbus.Publisher, workerID string, policy RetryPolicy, clk clock) (*Relay, error) {
	if repository == nil || publisher == nil || clk == nil {
		return nil, errors.New("outbox relay: repository, publisher and clock are required")
	}
	if !workerPattern.MatchString(workerID) {
		return nil, errors.New("outbox relay: invalid worker id")
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Relay{repository: repository, publisher: publisher, workerID: workerID, policy: policy, clock: clk}, nil
}

// RunOnce claims at most one batch for the supplied tenant scope and attempts
// each publication once. It returns the number of claimed rows, not successful
// publishes, so schedulers can distinguish an empty queue from work attempted.
func (relay *Relay) RunOnce(ctx context.Context, scope tenancy.Scope) (int, error) {
	if ctx == nil {
		return 0, errors.New("outbox relay: context is required")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if relay == nil || relay.repository == nil || relay.publisher == nil || relay.clock == nil {
		return 0, errors.New("outbox relay: relay is not initialized")
	}
	if !scope.Valid() {
		return 0, tenancy.ErrInvalidScope
	}
	claimed, err := relay.repository.Claim(ctx, scope, relay.workerID, relay.policy.BatchSize, relay.policy.LeaseDuration)
	if err != nil {
		return 0, fmt.Errorf("outbox relay: claim: %w", err)
	}
	for _, record := range claimed {
		if err := record.Validate(); err != nil {
			return len(claimed), err
		}
		if record.Event.OrganizationID != scope.OrganizationID().String() || record.Event.WorkspaceID != scope.WorkspaceID().String() {
			return len(claimed), ErrInvalidRecord
		}
		if err := ctx.Err(); err != nil {
			return len(claimed), err
		}
		publishErr := relay.publisher.Publish(ctx, record.Event)
		if publishErr == nil {
			publishedAt, err := domain.NewUTCInstant(relay.clock.Now().UTC())
			if err != nil {
				return len(claimed), err
			}
			if err := relay.repository.MarkPublished(ctx, scope, record.Event.ID, record.LeaseToken, publishedAt); err != nil {
				return len(claimed), fmt.Errorf("outbox relay: mark published: %w", err)
			}
			continue
		}
		if ctx.Err() != nil {
			return len(claimed), ctx.Err()
		}
		next, err := domain.NewUTCInstant(relay.clock.Now().UTC().Add(relay.retryDelay(record.Attempt)))
		if err != nil {
			return len(claimed), err
		}
		// Never persist arbitrary broker/client error text: it can contain
		// credentials or PII. Observability receives a stable machine code.
		if err := relay.repository.ReleaseForRetry(ctx, scope, record.Event.ID, record.LeaseToken, next, "publish_failed"); err != nil {
			return len(claimed), fmt.Errorf("outbox relay: schedule retry: %w", err)
		}
	}
	return len(claimed), nil
}

func (relay *Relay) retryDelay(attempt uint32) time.Duration {
	delay := relay.policy.BaseDelay
	// Saturating exponential backoff avoids shifts/Duration overflow.
	for current := uint32(1); current < attempt && delay < relay.policy.MaxDelay; current++ {
		if delay > relay.policy.MaxDelay/2 {
			return relay.policy.MaxDelay
		}
		delay *= 2
	}
	if delay > relay.policy.MaxDelay {
		return relay.policy.MaxDelay
	}
	return delay
}
