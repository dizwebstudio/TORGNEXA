package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

func testReportingEvent(organizationID, workspaceID, id string) eventbus.Event {
	return eventbus.Event{ID: id, OrganizationID: organizationID, WorkspaceID: workspaceID}
}

func TestReportingBatcherFlushesOnCount(t *testing.T) {
	var mu sync.Mutex
	var calls [][]eventbus.Event
	batcher := newReportingBatcher(3, time.Hour, func(_ context.Context, events []eventbus.Event) error {
		mu.Lock()
		calls = append(calls, events)
		mu.Unlock()
		return nil
	})

	ids := []string{"evt-1", "evt-2", "evt-3"}
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := batcher.submit(context.Background(), testReportingEvent("org-1", "ws-1", id)); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}(id)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 || len(calls[0]) != 3 {
		t.Fatalf("expected exactly one flush of 3 events, got %d flushes: %+v", len(calls), calls)
	}
}

func TestReportingBatcherFlushesOnTimer(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	batcher := newReportingBatcher(10, 20*time.Millisecond, func(_ context.Context, events []eventbus.Event) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	})

	if err := batcher.submit(context.Background(), testReportingEvent("org-1", "ws-1", "evt-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 1 {
		t.Fatalf("expected exactly one timer-triggered flush, got %d", got)
	}
}

func TestReportingBatcherGroupsByTenantAndReportsPerGroupError(t *testing.T) {
	const failingOrg = "org-bad"
	batcher := newReportingBatcher(4, time.Hour, func(_ context.Context, events []eventbus.Event) error {
		if len(events) == 0 {
			t.Fatalf("flush called with no events")
		}
		if events[0].OrganizationID == failingOrg {
			return errors.New("boom")
		}
		return nil
	})

	submissions := []eventbus.Event{
		testReportingEvent("org-good", "ws-1", "g-1"),
		testReportingEvent("org-good", "ws-1", "g-2"),
		testReportingEvent(failingOrg, "ws-1", "b-1"),
		testReportingEvent(failingOrg, "ws-1", "b-2"),
	}
	results := make([]error, len(submissions))
	var wg sync.WaitGroup
	for index, event := range submissions {
		wg.Add(1)
		go func(index int, event eventbus.Event) {
			defer wg.Done()
			results[index] = batcher.submit(context.Background(), event)
		}(index, event)
	}
	wg.Wait()

	if results[0] != nil || results[1] != nil {
		t.Fatalf("expected the good-tenant group to succeed, got %v and %v", results[0], results[1])
	}
	if results[2] == nil || results[3] == nil {
		t.Fatalf("expected the bad-tenant group to fail, got %v and %v", results[2], results[3])
	}
}

func TestReportingBatcherReturnsContextErrorWithoutFlushing(t *testing.T) {
	batcher := newReportingBatcher(10, time.Hour, func(context.Context, []eventbus.Event) error {
		t.Fatalf("ingest must not run before the batch fills or its timer fires")
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := batcher.submit(ctx, testReportingEvent("org-1", "ws-1", "evt-1")); err == nil {
		t.Fatalf("expected a context error for an already-canceled context")
	}
}
