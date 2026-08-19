package migration

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const (
	testJobID        = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0100"
	testOrganization = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	testWorkspace    = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
)

func TestRunnerCommitsOneBoundedBatch(t *testing.T) {
	t.Parallel()
	job := testBackfillJob(t)
	store := &fakeBackfillStore{}
	processor := ProcessorFunc(func(_ context.Context, lease Lease) (BatchResult, error) {
		if lease.Checkpoint != "" || lease.Generation != 1 {
			t.Fatalf("unexpected lease = %#v", lease)
		}
		return BatchResult{NextCheckpoint: "cursor-100", Processed: 100, Done: false}, nil
	})
	runner := mustRunner(t, store, processor)
	outcome, err := runner.RunOnce(context.Background(), job)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if outcome != (RunOutcome{Acquired: true, Processed: 100}) {
		t.Fatalf("RunOnce() outcome = %#v", outcome)
	}
	if store.commitCount != 1 || store.committed.NextCheckpoint != "cursor-100" || len(store.failures) != 0 {
		t.Fatalf("store state = %#v", store)
	}
}

func TestRunnerNoLeaseIsSuccessfulNoop(t *testing.T) {
	t.Parallel()
	store := &fakeBackfillStore{noWork: true}
	processorCalls := 0
	runner := mustRunner(t, store, ProcessorFunc(func(context.Context, Lease) (BatchResult, error) {
		processorCalls++
		return BatchResult{}, nil
	}))
	outcome, err := runner.RunOnce(context.Background(), testBackfillJob(t))
	if err != nil || outcome != (RunOutcome{}) || processorCalls != 0 {
		t.Fatalf("RunOnce() = %#v, %v; processor calls = %d", outcome, err, processorCalls)
	}
}

func TestRunnerSanitizesProcessorErrorAndPanic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		processor Processor
		wantError error
		wantCode  FailureCode
	}{
		{
			name: "error",
			processor: ProcessorFunc(func(context.Context, Lease) (BatchResult, error) {
				return BatchResult{}, errors.New("synthetic credential=must-not-escape")
			}),
			wantError: ErrProcessing,
			wantCode:  FailureProcessor,
		},
		{
			name: "panic",
			processor: ProcessorFunc(func(context.Context, Lease) (BatchResult, error) {
				panic("synthetic row payload must-not-escape")
			}),
			wantError: ErrProcessorPanic,
			wantCode:  FailureProcessorPanic,
		},
		{
			name: "goexit",
			processor: ProcessorFunc(func(context.Context, Lease) (BatchResult, error) {
				runtime.Goexit()
				return BatchResult{}, nil
			}),
			wantError: ErrProcessorPanic,
			wantCode:  FailureProcessorPanic,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeBackfillStore{}
			runner := mustRunner(t, store, test.processor)
			_, err := runner.RunOnce(context.Background(), testBackfillJob(t))
			if !errors.Is(err, test.wantError) {
				t.Fatalf("RunOnce() error = %v, want %v", err, test.wantError)
			}
			if strings.Contains(err.Error(), "must-not-escape") {
				t.Fatalf("RunOnce() leaked processor content: %v", err)
			}
			if len(store.failures) != 1 || store.failures[0] != test.wantCode || store.commitCount != 0 {
				t.Fatalf("failure state = %#v", store)
			}
		})
	}
}

func TestRunnerRejectsInvalidProgressAndUntrustedLease(t *testing.T) {
	t.Parallel()
	tests := []BatchResult{
		{NextCheckpoint: "", Processed: 1},
		{NextCheckpoint: "same", Processed: 1},
		{NextCheckpoint: "next", Processed: 101},
		{NextCheckpoint: "next\nunsafe", Processed: 1},
		{NextCheckpoint: "next", Processed: 0},
	}
	for _, result := range tests {
		result := result
		t.Run(result.NextCheckpoint+"/invalid", func(t *testing.T) {
			t.Parallel()
			store := &fakeBackfillStore{checkpoint: "same"}
			runner := mustRunner(t, store, ProcessorFunc(func(context.Context, Lease) (BatchResult, error) {
				return result, nil
			}))
			_, err := runner.RunOnce(context.Background(), testBackfillJob(t))
			if !errors.Is(err, ErrInvalidBackfill) || len(store.failures) != 1 || store.failures[0] != FailureInvalidResult {
				t.Fatalf("invalid result error/state = %v, %#v", err, store)
			}
		})
	}

	job := testBackfillJob(t)
	wrong := job
	wrong.Key = "synthetic.wrong"
	store := &fakeBackfillStore{leaseOverride: &Lease{Job: wrong, WorkerID: "worker-1", Generation: 1}}
	runner := mustRunner(t, store, ProcessorFunc(func(context.Context, Lease) (BatchResult, error) {
		t.Fatal("processor ran with an untrusted lease")
		return BatchResult{}, nil
	}))
	if _, err := runner.RunOnce(context.Background(), job); !errors.Is(err, ErrInvalidBackfill) || len(store.failures) != 0 {
		t.Fatalf("untrusted lease error/state = %v, %#v", err, store)
	}
}

func TestInterruptedCommitRetriesSameBatchIdempotently(t *testing.T) {
	t.Parallel()
	store := &fakeBackfillStore{commitErrors: []error{ErrLeaseLost, nil}}
	sideEffects := map[string]struct{}{}
	processorCalls := 0
	processor := ProcessorFunc(func(_ context.Context, lease Lease) (BatchResult, error) {
		processorCalls++
		if lease.Checkpoint != "" {
			t.Fatalf("retry checkpoint = %q, want last committed empty checkpoint", lease.Checkpoint)
		}
		for _, key := range []string{"row-1", "row-2"} {
			sideEffects[key] = struct{}{}
		}
		return BatchResult{NextCheckpoint: "row-2", Processed: 2, Done: true}, nil
	})
	runner := mustRunner(t, store, processor)
	job := testBackfillJob(t)
	if _, err := runner.RunOnce(context.Background(), job); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("interrupted RunOnce() error = %v", err)
	}
	outcome, err := runner.RunOnce(context.Background(), job)
	if err != nil || !outcome.Completed || outcome.Processed != 2 {
		t.Fatalf("retried RunOnce() = %#v, %v", outcome, err)
	}
	if processorCalls != 2 || len(sideEffects) != 2 || store.generation != 2 || store.commitCount != 1 {
		t.Fatalf("retry state: calls=%d effects=%d generation=%d commits=%d", processorCalls, len(sideEffects), store.generation, store.commitCount)
	}
}

func TestRunnerFailsClosedOnConfigurationAndCancellation(t *testing.T) {
	t.Parallel()
	processor := ProcessorFunc(func(context.Context, Lease) (BatchResult, error) { return BatchResult{}, nil })
	if _, err := NewRunner(nil, processor, "worker-1", time.Minute); !errors.Is(err, ErrInvalidBackfill) {
		t.Fatalf("NewRunner(nil) error = %v", err)
	}
	if _, err := NewRunner(&fakeBackfillStore{}, processor, "worker secret", time.Minute); !errors.Is(err, ErrInvalidBackfill) {
		t.Fatalf("NewRunner(unsafe worker) error = %v", err)
	}
	if _, err := NewRunner(&fakeBackfillStore{}, processor, "worker-1", time.Hour); !errors.Is(err, ErrInvalidBackfill) {
		t.Fatalf("NewRunner(long lease) error = %v", err)
	}

	store := &fakeBackfillStore{}
	runner := mustRunner(t, store, processor)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runner.RunOnce(canceled, testBackfillJob(t)); !errors.Is(err, context.Canceled) || store.acquireCount != 0 {
		t.Fatalf("canceled RunOnce() error/acquires = %v/%d", err, store.acquireCount)
	}
	invalidJob := testBackfillJob(t)
	invalidJob.BatchSize = 0
	if _, err := runner.RunOnce(context.Background(), invalidJob); !errors.Is(err, ErrInvalidBackfill) {
		t.Fatalf("invalid job error = %v", err)
	}
}

func TestRunnerCancellationLeavesLeaseForExpiry(t *testing.T) {
	t.Parallel()
	store := &fakeBackfillStore{}
	started := make(chan struct{})
	processor := ProcessorFunc(func(ctx context.Context, _ Lease) (BatchResult, error) {
		close(started)
		<-ctx.Done()
		return BatchResult{}, ctx.Err()
	})
	runner := mustRunner(t, store, processor)
	job := testBackfillJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runner.RunOnce(ctx, job)
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RunOnce() cancellation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RunOnce() ignored cancellation")
	}
	if len(store.failures) != 0 || store.commitCount != 0 {
		t.Fatalf("canceled runner persisted stale state: %#v", store)
	}
}

type fakeBackfillStore struct {
	noWork        bool
	checkpoint    string
	generation    int64
	acquireCount  int
	commitCount   int
	committed     BatchResult
	commitErrors  []error
	failures      []FailureCode
	leaseOverride *Lease
}

func (store *fakeBackfillStore) Acquire(_ context.Context, job BackfillJob, worker string, _ time.Duration) (Lease, bool, error) {
	store.acquireCount++
	if store.noWork {
		return Lease{}, false, nil
	}
	if store.leaseOverride != nil {
		return *store.leaseOverride, true, nil
	}
	store.generation++
	return Lease{Job: job, WorkerID: worker, Generation: store.generation, Checkpoint: store.checkpoint}, true, nil
}

func (store *fakeBackfillStore) Commit(_ context.Context, _ Lease, result BatchResult) error {
	if len(store.commitErrors) > 0 {
		err := store.commitErrors[0]
		store.commitErrors = store.commitErrors[1:]
		if err != nil {
			return err
		}
	}
	store.commitCount++
	store.committed = result
	store.checkpoint = result.NextCheckpoint
	return nil
}

func (store *fakeBackfillStore) Fail(_ context.Context, _ Lease, code FailureCode) error {
	store.failures = append(store.failures, code)
	return nil
}

func mustRunner(t *testing.T, store Store, processor Processor) *Runner {
	t.Helper()
	runner, err := NewRunner(store, processor, "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	return runner
}

func testBackfillJob(t *testing.T) BackfillJob {
	t.Helper()
	scope, err := tenancy.ParseScope(testOrganization, testWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	backfillScope, err := WorkspaceBackfillScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	return BackfillJob{ID: testJobID, Key: "synthetic.backfill", Scope: backfillScope, BatchSize: 100}
}
