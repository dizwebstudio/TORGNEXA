package migration

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

const (
	maxCheckpointBytes = 512
	maxBatchSize       = 10000
	minLeaseDuration   = time.Second
	maxLeaseDuration   = 15 * time.Minute
)

var (
	workerIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`)
	// ErrInvalidBackfill means runner configuration, persisted lease data, or a
	// processor result violated the bounded backfill contract.
	ErrInvalidBackfill = errors.New("invalid backfill state")
	// ErrProcessing is a sanitized processor failure. The original processor
	// error is intentionally not returned because it may contain row content.
	ErrProcessing = errors.New("backfill processing failed")
	// ErrProcessorPanic is a sanitized processor panic. Panic values never cross
	// the runner boundary or enter checkpoint storage.
	ErrProcessorPanic = errors.New("backfill processor panic")
	// ErrLeaseLost means a stale worker was fenced from committing progress.
	ErrLeaseLost = errors.New("backfill lease lost")
)

// FailureCode is a stable, non-sensitive checkpoint failure classification.
type FailureCode string

const (
	FailureProcessor      FailureCode = "processor_failed"
	FailureProcessorPanic FailureCode = "processor_panic"
	FailureInvalidResult  FailureCode = "invalid_batch_result"
)

// BackfillScope is either global or one validated organization/workspace pair.
// Global jobs require a separately reviewed migration role; application roles
// must never infer global access from an empty tenant context.
type BackfillScope struct {
	global       bool
	organization tenancy.OrganizationID
	workspace    tenancy.WorkspaceID
}

// GlobalBackfillScope returns the explicit privileged/global scope.
func GlobalBackfillScope() BackfillScope {
	return BackfillScope{global: true}
}

// WorkspaceBackfillScope converts an authenticated validated tenant scope.
func WorkspaceBackfillScope(scope tenancy.Scope) (BackfillScope, error) {
	if !scope.Valid() {
		return BackfillScope{}, ErrInvalidBackfill
	}
	return BackfillScope{organization: scope.OrganizationID(), workspace: scope.WorkspaceID()}, nil
}

// Global reports whether this job requires the privileged global runner.
func (scope BackfillScope) Global() bool { return scope.global }

// OrganizationID returns the tenant organization, or an invalid zero value for
// an explicitly global scope.
func (scope BackfillScope) OrganizationID() tenancy.OrganizationID { return scope.organization }

// WorkspaceID returns the tenant workspace, or an invalid zero value for an
// explicitly global scope.
func (scope BackfillScope) WorkspaceID() tenancy.WorkspaceID { return scope.workspace }

func (scope BackfillScope) valid() bool {
	if scope.global {
		return !scope.organization.Valid() && !scope.workspace.Valid()
	}
	return scope.organization.Valid() && scope.workspace.Valid()
}

// BackfillJob is immutable registration metadata for one resumable job scope.
type BackfillJob struct {
	ID        string
	Key       string
	Scope     BackfillScope
	BatchSize int
}

func (job BackfillJob) valid() bool {
	return domain.ValidSortableID(job.ID) && jobKeyPattern.MatchString(job.Key) &&
		job.Scope.valid() && job.BatchSize >= 1 && job.BatchSize <= maxBatchSize
}

// Valid reports whether job registration is bounded and canonically scoped.
func (job BackfillJob) Valid() bool { return job.valid() }

// Lease is a fenced, expiring right to process one batch from Checkpoint.
type Lease struct {
	Job        BackfillJob
	WorkerID   string
	Generation int64
	Checkpoint string
}

func (lease Lease) validFor(job BackfillJob, workerID string) bool {
	return lease.Job == job && lease.WorkerID == workerID && lease.Generation >= 1 && validCheckpoint(lease.Checkpoint)
}

// BatchResult is the only processor output persisted as progress. Processed is
// bounded by the registered batch size. A non-final batch must advance a stable
// cursor; processors are retry-idempotent because a crash may repeat a batch.
type BatchResult struct {
	NextCheckpoint string
	Processed      int
	Done           bool
}

// ValidFor reports whether a result can safely advance this fenced lease.
func (result BatchResult) ValidFor(lease Lease) bool {
	return validateBatchResult(lease, result) == nil
}

// ValidWorkerID reports whether a worker identifier is safe for checkpoint
// metadata. It is an identifier, never a hostname, credential, or free text.
func ValidWorkerID(value string) bool { return workerIDPattern.MatchString(value) }

// ValidLeaseDuration reports whether a lease remains within runner bounds.
func ValidLeaseDuration(value time.Duration) bool {
	return value >= minLeaseDuration && value <= maxLeaseDuration
}

// ValidCheckpoint reports whether a cursor is bounded, trimmed, valid UTF-8,
// and contains no control character.
func ValidCheckpoint(value string) bool { return validCheckpoint(value) }

// Valid reports whether this failure code is one of the non-sensitive values
// emitted by Runner.
func (code FailureCode) Valid() bool {
	return code == FailureProcessor || code == FailureProcessorPanic || code == FailureInvalidResult
}

// Store persists job registration, fenced leases, checkpoints, counts, and
// sanitized failure codes. Commit and Fail must compare worker plus generation
// and return ErrLeaseLost for a stale lease.
type Store interface {
	Acquire(context.Context, BackfillJob, string, time.Duration) (Lease, bool, error)
	Commit(context.Context, Lease, BatchResult) error
	Fail(context.Context, Lease, FailureCode) error
}

// Processor performs one bounded, retry-idempotent batch after the lease
// checkpoint. It must not place record content or credentials in returned
// errors or cursors and must stop promptly when the context is canceled.
type Processor interface {
	Process(context.Context, Lease) (BatchResult, error)
}

// ProcessorFunc adapts a function to Processor.
type ProcessorFunc func(context.Context, Lease) (BatchResult, error)

// Process executes the adapted function.
func (function ProcessorFunc) Process(ctx context.Context, lease Lease) (BatchResult, error) {
	return function(ctx, lease)
}

// RunOutcome reports one scheduler tick without exposing row content.
type RunOutcome struct {
	Acquired  bool
	Completed bool
	Processed int
}

// Runner executes at most one bounded batch per invocation. Scheduling and
// retries remain external, which prevents an unbounded in-process migration.
type Runner struct {
	store         Store
	processor     Processor
	workerID      string
	leaseDuration time.Duration
}

// NewRunner constructs a fail-closed resumable backfill runner.
func NewRunner(store Store, processor Processor, workerID string, leaseDuration time.Duration) (*Runner, error) {
	if store == nil || processor == nil || !workerIDPattern.MatchString(workerID) || leaseDuration < minLeaseDuration || leaseDuration > maxLeaseDuration {
		return nil, ErrInvalidBackfill
	}
	return &Runner{store: store, processor: processor, workerID: workerID, leaseDuration: leaseDuration}, nil
}

// RunOnce claims and processes at most one batch. No available lease is a
// successful no-op. Interrupted work is retried from its last committed cursor.
func (runner *Runner) RunOnce(ctx context.Context, job BackfillJob) (RunOutcome, error) {
	if ctx == nil {
		return RunOutcome{}, errors.New("backfill runner: context is required")
	}
	if err := ctx.Err(); err != nil {
		return RunOutcome{}, fmt.Errorf("backfill runner: %w", err)
	}
	if runner == nil || runner.store == nil || runner.processor == nil || !job.valid() {
		return RunOutcome{}, ErrInvalidBackfill
	}
	lease, acquired, err := runner.store.Acquire(ctx, job, runner.workerID, runner.leaseDuration)
	if err != nil {
		return RunOutcome{}, fmt.Errorf("backfill acquire: %w", err)
	}
	if !acquired {
		return RunOutcome{}, nil
	}
	if !lease.validFor(job, runner.workerID) {
		return RunOutcome{Acquired: true}, ErrInvalidBackfill
	}
	result, processorErr := runProcessor(ctx, runner.processor, lease)
	if processorErr != nil {
		if err := ctx.Err(); err != nil {
			return RunOutcome{Acquired: true}, fmt.Errorf("backfill runner: %w", err)
		}
		code := FailureProcessor
		publicErr := ErrProcessing
		if errors.Is(processorErr, ErrProcessorPanic) {
			code = FailureProcessorPanic
			publicErr = ErrProcessorPanic
		}
		if err := runner.store.Fail(ctx, lease, code); err != nil {
			return RunOutcome{Acquired: true}, errors.Join(publicErr, fmt.Errorf("record backfill failure: %w", err))
		}
		return RunOutcome{Acquired: true}, publicErr
	}
	if err := validateBatchResult(lease, result); err != nil {
		if failureErr := runner.store.Fail(ctx, lease, FailureInvalidResult); failureErr != nil {
			return RunOutcome{Acquired: true}, errors.Join(ErrInvalidBackfill, fmt.Errorf("record invalid backfill result: %w", failureErr))
		}
		return RunOutcome{Acquired: true}, err
	}
	if err := runner.store.Commit(ctx, lease, result); err != nil {
		return RunOutcome{Acquired: true}, fmt.Errorf("commit backfill checkpoint: %w", err)
	}
	return RunOutcome{Acquired: true, Completed: result.Done, Processed: result.Processed}, nil
}

func runProcessor(ctx context.Context, processor Processor, lease Lease) (result BatchResult, err error) {
	type response struct {
		result BatchResult
		err    error
	}
	responses := make(chan response, 1)
	go func() {
		completed := false
		defer func() {
			if recovered := recover(); recovered != nil {
				responses <- response{err: ErrProcessorPanic}
				return
			}
			if !completed {
				// runtime.Goexit runs deferred functions without a recoverable
				// panic. Treat it as the same sanitized processor failure.
				responses <- response{err: ErrProcessorPanic}
			}
		}()
		batch, processErr := processor.Process(ctx, lease)
		completed = true
		responses <- response{result: batch, err: processErr}
	}()
	select {
	case <-ctx.Done():
		return BatchResult{}, ctx.Err()
	case outcome := <-responses:
		return outcome.result, outcome.err
	}
}

func validateBatchResult(lease Lease, result BatchResult) error {
	if result.Processed < 0 || result.Processed > lease.Job.BatchSize || !validCheckpoint(result.NextCheckpoint) {
		return ErrInvalidBackfill
	}
	if !result.Done && (result.Processed == 0 || result.NextCheckpoint == "" || result.NextCheckpoint == lease.Checkpoint) {
		return ErrInvalidBackfill
	}
	if result.Processed > 0 && result.NextCheckpoint == "" {
		return ErrInvalidBackfill
	}
	return nil
}

func validCheckpoint(value string) bool {
	if !utf8.ValidString(value) || len(value) > maxCheckpointBytes || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
