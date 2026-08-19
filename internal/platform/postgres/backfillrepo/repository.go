// Package backfillrepo persists resumable migration backfill leases and
// checkpoints in PostgreSQL without depending on a concrete database driver.
package backfillrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/migration"
)

const ensureJobStatement = `INSERT INTO backfill_jobs (
  id, job_key, organization_id, workspace_id, batch_size
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO NOTHING`

const jobMetadataQuery = `SELECT
  job_key, organization_id, workspace_id, batch_size
FROM backfill_jobs
WHERE id = $1
FOR UPDATE`

const claimStatement = `UPDATE backfill_jobs
SET state = 'running',
    lease_owner = $6,
    lease_until = clock_timestamp() + ($7 * interval '1 millisecond'),
    lease_generation = lease_generation + 1,
    attempts = attempts + 1,
    last_error_code = NULL,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = $1
  AND job_key = $2
  AND organization_id IS NOT DISTINCT FROM $3
  AND workspace_id IS NOT DISTINCT FROM $4
  AND batch_size = $5
  AND (
    state IN ('pending', 'failed')
    OR (state = 'running' AND lease_until <= clock_timestamp())
  )
RETURNING checkpoint, lease_generation`

const commitStatement = `UPDATE backfill_jobs
SET checkpoint = $8,
    processed_count = processed_count + $9,
    state = CASE WHEN $10 THEN 'completed' ELSE 'pending' END,
    lease_owner = NULL,
    lease_until = NULL,
    last_error_code = NULL,
    completed_at = CASE WHEN $10 THEN clock_timestamp() ELSE NULL END,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = $1
  AND job_key = $2
  AND organization_id IS NOT DISTINCT FROM $3
  AND workspace_id IS NOT DISTINCT FROM $4
  AND batch_size = $5
  AND state = 'running'
  AND lease_owner = $6
  AND lease_generation = $7
  AND lease_until > clock_timestamp()`

const failStatement = `UPDATE backfill_jobs
SET state = 'failed',
    lease_owner = NULL,
    lease_until = NULL,
    last_error_code = $8,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = $1
  AND job_key = $2
  AND organization_id IS NOT DISTINCT FROM $3
  AND workspace_id IS NOT DISTINCT FROM $4
  AND batch_size = $5
  AND state = 'running'
  AND lease_owner = $6
  AND lease_generation = $7
  AND lease_until > clock_timestamp()`

// Repository is a PostgreSQL implementation of migration.Store. Its database
// must use the separately reviewed migration role, never an application role.
type Repository struct {
	transactions transactor
}

var _ migration.Store = (*Repository)(nil)

// New constructs a PostgreSQL backfill repository.
func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("backfill repository: database is required")
	}
	return newRepository(sqlTransactor{database: database}), nil
}

func newRepository(transactions transactor) *Repository {
	return &Repository{transactions: transactions}
}

// Acquire registers immutable job metadata if absent and atomically claims an
// available/expired lease. Paused and completed jobs return acquired=false.
func (repository *Repository) Acquire(ctx context.Context, job migration.BackfillJob, workerID string, duration time.Duration) (migration.Lease, bool, error) {
	if err := validateContextRepository(ctx, repository); err != nil {
		return migration.Lease{}, false, err
	}
	if !job.Valid() || !migration.ValidWorkerID(workerID) || !migration.ValidLeaseDuration(duration) {
		return migration.Lease{}, false, migration.ErrInvalidBackfill
	}
	organization, workspace := scopeArguments(job.Scope)
	var lease migration.Lease
	acquired := false
	err := repository.transactions.within(ctx, func(queries queryer) error {
		if _, err := queries.ExecContext(ctx, ensureJobStatement, job.ID, job.Key, organization, workspace, job.BatchSize); err != nil {
			return fmt.Errorf("register backfill job: %w", err)
		}
		var storedKey string
		var storedOrganization, storedWorkspace sql.NullString
		var storedBatch int
		if err := queries.QueryRowContext(ctx, jobMetadataQuery, job.ID).Scan(
			&storedKey,
			&storedOrganization,
			&storedWorkspace,
			&storedBatch,
		); err != nil {
			return fmt.Errorf("read backfill registration: %w", err)
		}
		if storedKey != job.Key || storedBatch != job.BatchSize || !scopeMatches(job.Scope, storedOrganization, storedWorkspace) {
			return migration.ErrInvalidBackfill
		}
		var checkpoint string
		var generation int64
		err := queries.QueryRowContext(
			ctx,
			claimStatement,
			job.ID,
			job.Key,
			organization,
			workspace,
			job.BatchSize,
			workerID,
			duration.Milliseconds(),
		).Scan(&checkpoint, &generation)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("claim backfill lease: %w", err)
		}
		if generation < 1 || !migration.ValidCheckpoint(checkpoint) {
			return migration.ErrInvalidBackfill
		}
		lease = migration.Lease{Job: job, WorkerID: workerID, Generation: generation, Checkpoint: checkpoint}
		acquired = true
		return nil
	})
	if err != nil {
		return migration.Lease{}, false, err
	}
	return lease, acquired, nil
}

// Commit advances one checkpoint only while worker and generation still own an
// unexpired lease. Rows affected other than one are treated as fencing loss.
func (repository *Repository) Commit(ctx context.Context, lease migration.Lease, result migration.BatchResult) error {
	if err := validateContextRepository(ctx, repository); err != nil {
		return err
	}
	if !validLease(lease) || !result.ValidFor(lease) {
		return migration.ErrInvalidBackfill
	}
	organization, workspace := scopeArguments(lease.Job.Scope)
	return repository.transactions.within(ctx, func(queries queryer) error {
		resultValue, err := queries.ExecContext(
			ctx,
			commitStatement,
			lease.Job.ID,
			lease.Job.Key,
			organization,
			workspace,
			lease.Job.BatchSize,
			lease.WorkerID,
			lease.Generation,
			result.NextCheckpoint,
			result.Processed,
			result.Done,
		)
		if err != nil {
			return fmt.Errorf("commit backfill progress: %w", err)
		}
		return requireOneAffected(resultValue)
	})
}

// Fail records only a stable error code while fencing stale workers.
func (repository *Repository) Fail(ctx context.Context, lease migration.Lease, code migration.FailureCode) error {
	if err := validateContextRepository(ctx, repository); err != nil {
		return err
	}
	if !validLease(lease) || !code.Valid() {
		return migration.ErrInvalidBackfill
	}
	organization, workspace := scopeArguments(lease.Job.Scope)
	return repository.transactions.within(ctx, func(queries queryer) error {
		resultValue, err := queries.ExecContext(
			ctx,
			failStatement,
			lease.Job.ID,
			lease.Job.Key,
			organization,
			workspace,
			lease.Job.BatchSize,
			lease.WorkerID,
			lease.Generation,
			string(code),
		)
		if err != nil {
			return fmt.Errorf("record backfill failure: %w", err)
		}
		return requireOneAffected(resultValue)
	})
}

func validateContextRepository(ctx context.Context, repository *Repository) error {
	if ctx == nil {
		return errors.New("backfill repository: context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("backfill repository: %w", err)
	}
	if repository == nil || repository.transactions == nil {
		return errors.New("backfill repository: repository is not initialized")
	}
	return nil
}

func validLease(lease migration.Lease) bool {
	return lease.Job.Valid() && migration.ValidWorkerID(lease.WorkerID) && lease.Generation >= 1 && migration.ValidCheckpoint(lease.Checkpoint)
}

func scopeArguments(scope migration.BackfillScope) (any, any) {
	if scope.Global() {
		return nil, nil
	}
	return scope.OrganizationID().String(), scope.WorkspaceID().String()
}

func scopeMatches(scope migration.BackfillScope, organization, workspace sql.NullString) bool {
	if scope.Global() {
		return !organization.Valid && !workspace.Valid
	}
	return organization.Valid && workspace.Valid && organization.String == scope.OrganizationID().String() && workspace.String == scope.WorkspaceID().String()
}

func requireOneAffected(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected backfill rows: %w", err)
	}
	if rows != 1 {
		return migration.ErrLeaseLost
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

type queryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) rowScanner
}

type transactor interface {
	within(context.Context, func(queryer) error) error
}

type sqlTransactor struct {
	database *sql.DB
}

func (transactions sqlTransactor) within(ctx context.Context, operation func(queryer) error) error {
	transaction, err := transactions.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin backfill transaction: %w", err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = transaction.Rollback()
		}
	}()
	queries := sqlQueries{transaction: transaction}
	if err := operation(queries); err != nil {
		rollbackErr := transaction.Rollback()
		finished = true
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return errors.Join(err, fmt.Errorf("rollback backfill transaction: %w", rollbackErr))
		}
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit backfill transaction: %w", err)
	}
	finished = true
	return nil
}

type sqlQueries struct {
	transaction *sql.Tx
}

func (queries sqlQueries) ExecContext(ctx context.Context, statement string, arguments ...any) (sql.Result, error) {
	return queries.transaction.ExecContext(ctx, statement, arguments...)
}

func (queries sqlQueries) QueryRowContext(ctx context.Context, statement string, arguments ...any) rowScanner {
	return queries.transaction.QueryRowContext(ctx, statement, arguments...)
}
