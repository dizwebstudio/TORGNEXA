package worker

import (
	"context"
	"sync"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

// reportingBatchMaxItems and reportingBatchMaxDelay bound how long a Kafka
// message can sit buffered in-process before its ClickHouse insert happens.
// Both are deliberately small relative to reporting.MaxBatchSize: this is a
// latency-bounded micro-batch for throughput, not a bulk loader.
const (
	reportingBatchMaxItems = 200
	reportingBatchMaxDelay = 500 * time.Millisecond
)

type reportingBatchItem struct {
	event eventbus.Event
	done  chan error
}

// reportingBatcher coalesces individual Kafka-delivered events into fewer,
// larger ClickHouse inserts. reporting.Sink's contract requires acknowledging
// a record only once ClickHouse has durably accepted it (see
// internal/platform/reporting.Sink), so submit blocks the caller until its
// event has actually been included in a completed flush rather than
// returning early.
//
// reporting.NewBatch rejects a batch mixing more than one tenant, so every
// flush groups pending events by (organization_id, workspace_id) first.
type reportingBatcher struct {
	ingest   func(ctx context.Context, events []eventbus.Event) error
	maxItems int
	maxDelay time.Duration

	mu      sync.Mutex
	pending []reportingBatchItem
	timer   *time.Timer
}

func newReportingBatcher(maxItems int, maxDelay time.Duration, ingest func(context.Context, []eventbus.Event) error) *reportingBatcher {
	return &reportingBatcher{ingest: ingest, maxItems: maxItems, maxDelay: maxDelay}
}

// submit enqueues event and waits for the flush it ends up part of. Returning
// ctx.Err() when the caller's context ends does not cancel that flush for
// other still-waiting callers: it only stops this call from waiting on it.
func (b *reportingBatcher) submit(ctx context.Context, event eventbus.Event) error {
	item := reportingBatchItem{event: event, done: make(chan error, 1)}

	b.mu.Lock()
	b.pending = append(b.pending, item)
	var readyToFlush []reportingBatchItem
	if len(b.pending) >= b.maxItems {
		readyToFlush = b.pending
		b.pending = nil
		if b.timer != nil {
			b.timer.Stop()
			b.timer = nil
		}
	} else if b.timer == nil {
		b.timer = time.AfterFunc(b.maxDelay, b.flushPending)
	}
	b.mu.Unlock()

	if readyToFlush != nil {
		b.flush(readyToFlush)
	}

	select {
	case err := <-item.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *reportingBatcher) flushPending() {
	b.mu.Lock()
	items := b.pending
	b.pending = nil
	b.timer = nil
	b.mu.Unlock()
	if len(items) > 0 {
		b.flush(items)
	}
}

func (b *reportingBatcher) flush(items []reportingBatchItem) {
	type tenantKey struct{ organizationID, workspaceID string }
	groups := make(map[tenantKey][]int, len(items))
	for index, item := range items {
		key := tenantKey{item.event.OrganizationID, item.event.WorkspaceID}
		groups[key] = append(groups[key], index)
	}
	for _, indices := range groups {
		events := make([]eventbus.Event, len(indices))
		for position, index := range indices {
			events[position] = items[index].event
		}
		// Flushes are shared across callers with independent contexts, so no
		// single caller's context is the right one here; reporting.Sink's own
		// implementations (e.g. ClickHouseSink) apply their own bounded
		// timeout regardless of the context they are given.
		err := b.ingest(context.Background(), events)
		for _, index := range indices {
			items[index].done <- err
		}
	}
}
