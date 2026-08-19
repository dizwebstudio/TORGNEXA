// Package outboxrepo implements the PostgreSQL transactional outbox.
package outboxrepo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/outbox"
)

var safeCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

const verifyTxScopeStatement = `SELECT
  current_setting('app.organization_id', true),
  current_setting('app.workspace_id', true)`

const applyScopeStatement = `SELECT
  set_config('app.organization_id', $1, true),
  set_config('app.workspace_id', $2, true)`

const enqueueStatement = `INSERT INTO outbox_events (
  id, organization_id, workspace_id, event_type, aggregate_type, aggregate_id,
  payload, event_envelope, available_at
) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,clock_timestamp())
ON CONFLICT (id) DO NOTHING
RETURNING id`

const duplicateCheckStatement = `SELECT event_envelope = $2::jsonb
FROM outbox_events
WHERE id = $1`

const legacyPendingStatement = `SELECT EXISTS (
  SELECT 1 FROM outbox_events
  WHERE organization_id = $1
    AND workspace_id = $2
    AND published_at IS NULL
    AND event_envelope IS NULL
)`

const claimStatement = `WITH candidates AS (
  SELECT id
  FROM outbox_events
  WHERE organization_id = $1
    AND workspace_id = $2
    AND published_at IS NULL
    AND event_envelope IS NOT NULL
    AND available_at <= clock_timestamp()
    AND (lease_expires_at IS NULL OR lease_expires_at <= clock_timestamp())
  ORDER BY created_at, id
  FOR UPDATE SKIP LOCKED
  LIMIT $6
)
UPDATE outbox_events AS o
SET lease_owner = $3,
    lease_token = $4,
    lease_expires_at = clock_timestamp() + ($5::bigint * interval '1 millisecond'),
    attempts = o.attempts + 1,
    last_attempt_at = clock_timestamp(),
    last_error_code = NULL
FROM candidates AS c
WHERE o.id = c.id
RETURNING o.event_envelope::text, o.attempts, o.lease_token, o.lease_owner, o.lease_expires_at`

const markPublishedStatement = `UPDATE outbox_events
SET published_at = $5,
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    last_error_code = NULL
WHERE organization_id = $1
  AND workspace_id = $2
  AND id = $3
  AND lease_token = $4
  AND published_at IS NULL
  AND lease_expires_at > clock_timestamp()`

const releaseForRetryStatement = `UPDATE outbox_events
SET available_at = $5,
    last_error_code = $6,
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL
WHERE organization_id = $1
  AND workspace_id = $2
  AND id = $3
  AND lease_token = $4
  AND published_at IS NULL
  AND lease_expires_at > clock_timestamp()`

// TransactionEnqueuer is bound to a caller-owned *sql.Tx. It never commits or
// rolls back; therefore a domain mutation and its event intent share fate.
type TransactionEnqueuer struct{ tx txQueryer }

var _ outbox.Enqueuer = (*TransactionEnqueuer)(nil)

func NewTransactionEnqueuer(tx *sql.Tx) (*TransactionEnqueuer, error) {
	if tx == nil {
		return nil, errors.New("outbox enqueue: transaction is required")
	}
	return &TransactionEnqueuer{tx: sqlTxQueryer{tx: tx}}, nil
}

func newTransactionEnqueuer(tx txQueryer) *TransactionEnqueuer { return &TransactionEnqueuer{tx: tx} }

func (enqueuer *TransactionEnqueuer) Enqueue(ctx context.Context, event eventbus.Event) error {
	if ctx == nil {
		return errors.New("outbox enqueue: context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if enqueuer == nil || enqueuer.tx == nil {
		return errors.New("outbox enqueue: enqueuer is not initialized")
	}
	if err := event.Validate(); err != nil {
		return fmt.Errorf("outbox enqueue: %w: %v", outbox.ErrInvalidRecord, err)
	}
	scope, err := tenancy.ParseScope(event.OrganizationID, event.WorkspaceID)
	if err != nil {
		return fmt.Errorf("outbox enqueue: %w", outbox.ErrInvalidRecord)
	}

	var organizationID, workspaceID sql.NullString
	if err := enqueuer.tx.QueryRowContext(ctx, verifyTxScopeStatement).Scan(&organizationID, &workspaceID); err != nil {
		return fmt.Errorf("outbox enqueue: verify transaction tenant scope: %w", err)
	}
	if !organizationID.Valid || !workspaceID.Valid || organizationID.String != scope.OrganizationID().String() || workspaceID.String != scope.WorkspaceID().String() {
		return fmt.Errorf("outbox enqueue: transaction tenant scope mismatch: %w", tenancy.ErrInvalidScope)
	}

	envelope, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("outbox enqueue: encode event: %w", err)
	}
	var insertedID string
	err = enqueuer.tx.QueryRowContext(ctx, enqueueStatement,
		event.ID, event.OrganizationID, event.WorkspaceID, event.Type.String(), event.EntityType, event.EntityID,
		string(event.Data), string(envelope),
	).Scan(&insertedID)
	if err == nil {
		if insertedID != event.ID {
			return outbox.ErrInvalidRecord
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("outbox enqueue: insert: %w", err)
	}

	// A command retry may enqueue the exact same immutable event ID. Treat that
	// as idempotent, but reject ID reuse with different bytes/metadata.
	var identical bool
	if err := enqueuer.tx.QueryRowContext(ctx, duplicateCheckStatement, event.ID, string(envelope)).Scan(&identical); err != nil {
		return fmt.Errorf("outbox enqueue: inspect duplicate: %w", err)
	}
	if !identical {
		return fmt.Errorf("outbox enqueue: event id collision: %w", outbox.ErrInvalidRecord)
	}
	return nil
}

// Repository performs tenant-scoped relay operations using short DB transactions.
type Repository struct {
	transactions transactor
	random       io.Reader
}

var _ outbox.Repository = (*Repository)(nil)

func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("outbox repository: database is required")
	}
	return newRepository(sqlTransactor{database: database}, rand.Reader), nil
}
func newRepository(transactions transactor, random io.Reader) *Repository {
	return &Repository{transactions: transactions, random: random}
}

func (repository *Repository) Claim(ctx context.Context, scope tenancy.Scope, workerID string, batchSize int, lease time.Duration) ([]outbox.Claimed, error) {
	if err := validateOperation(ctx, repository, scope); err != nil {
		return nil, err
	}
	if !workerPattern.MatchString(workerID) || batchSize < 1 || batchSize > 1000 || lease < time.Second || lease > 10*time.Minute {
		return nil, outbox.ErrInvalidRecord
	}
	token, err := leaseToken(repository.random)
	if err != nil {
		return nil, fmt.Errorf("outbox repository: lease token: %w", err)
	}
	claimed := make([]outbox.Claimed, 0, batchSize)
	err = repository.transactions.readWrite(ctx, func(queries queryer) error {
		if err := applyScope(ctx, queries, scope); err != nil {
			return err
		}
		var legacyPending bool
		if err := queries.QueryRowContext(ctx, legacyPendingStatement, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&legacyPending); err != nil {
			return fmt.Errorf("inspect legacy outbox rows: %w", err)
		}
		if legacyPending {
			return outbox.ErrLegacyRows
		}
		rows, err := queries.QueryContext(ctx, claimStatement, scope.OrganizationID().String(), scope.WorkspaceID().String(), workerID, token, lease.Milliseconds(), batchSize)
		if err != nil {
			return fmt.Errorf("claim outbox rows: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			var attempt int64
			var leaseTokenValue, leaseOwner string
			var leaseUntil time.Time
			if err := rows.Scan(&raw, &attempt, &leaseTokenValue, &leaseOwner, &leaseUntil); err != nil {
				return fmt.Errorf("scan claimed outbox row: %w", err)
			}
			if attempt < 1 || attempt > int64(^uint32(0)) {
				return outbox.ErrInvalidRecord
			}
			var event eventbus.Event
			if err := event.UnmarshalJSON([]byte(raw)); err != nil {
				return fmt.Errorf("decode claimed outbox event: %w", outbox.ErrInvalidRecord)
			}
			if event.OrganizationID != scope.OrganizationID().String() || event.WorkspaceID != scope.WorkspaceID().String() {
				return outbox.ErrInvalidRecord
			}
			instant, err := domain.NewUTCInstant(leaseUntil.UTC())
			if err != nil {
				return outbox.ErrInvalidRecord
			}
			record := outbox.Claimed{Event: event, Attempt: uint32(attempt), LeaseToken: leaseTokenValue, LeaseOwner: leaseOwner, LeaseUntil: instant}
			if err := record.Validate(); err != nil {
				return err
			}
			claimed = append(claimed, record)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate claimed outbox rows: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (repository *Repository) MarkPublished(ctx context.Context, scope tenancy.Scope, eventID, token string, publishedAt domain.UTCInstant) error {
	if err := validateOperation(ctx, repository, scope); err != nil {
		return err
	}
	if !identifierPattern.MatchString(eventID) || !leaseTokenPattern.MatchString(token) || publishedAt.Validate() != nil {
		return outbox.ErrInvalidRecord
	}
	return repository.updateLease(ctx, scope, markPublishedStatement, eventID, token, publishedAt.Time())
}

func (repository *Repository) ReleaseForRetry(ctx context.Context, scope tenancy.Scope, eventID, token string, availableAt domain.UTCInstant, code string) error {
	if err := validateOperation(ctx, repository, scope); err != nil {
		return err
	}
	if !identifierPattern.MatchString(eventID) || !leaseTokenPattern.MatchString(token) || availableAt.Validate() != nil || !safeCodePattern.MatchString(code) {
		return outbox.ErrInvalidRecord
	}
	return repository.updateLease(ctx, scope, releaseForRetryStatement, eventID, token, availableAt.Time(), code)
}

func (repository *Repository) updateLease(ctx context.Context, scope tenancy.Scope, statement, eventID, token string, extra ...any) error {
	return repository.transactions.readWrite(ctx, func(queries queryer) error {
		if err := applyScope(ctx, queries, scope); err != nil {
			return err
		}
		args := []any{scope.OrganizationID().String(), scope.WorkspaceID().String(), eventID, token}
		args = append(args, extra...)
		result, err := queries.ExecContext(ctx, statement, args...)
		if err != nil {
			return fmt.Errorf("update outbox lease: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("update outbox lease result: %w", err)
		}
		if rows != 1 {
			return outbox.ErrLeaseLost
		}
		return nil
	})
}

func validateOperation(ctx context.Context, repository *Repository, scope tenancy.Scope) error {
	if ctx == nil {
		return errors.New("outbox repository: context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if repository == nil || repository.transactions == nil || repository.random == nil {
		return errors.New("outbox repository: repository is not initialized")
	}
	if !scope.Valid() {
		return tenancy.ErrInvalidScope
	}
	return nil
}

func applyScope(ctx context.Context, queries queryer, scope tenancy.Scope) error {
	var organizationID, workspaceID string
	if err := queries.QueryRowContext(ctx, applyScopeStatement, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&organizationID, &workspaceID); err != nil {
		return fmt.Errorf("apply outbox tenant scope: %w", err)
	}
	if organizationID != scope.OrganizationID().String() || workspaceID != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	return nil
}

func leaseToken(random io.Reader) (string, error) {
	if random == nil {
		return "", errors.New("random source is required")
	}
	var raw [16]byte
	if _, err := io.ReadFull(random, raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

var (
	workerPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	leaseTokenPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

type rowScanner interface{ Scan(...any) error }
type rowsScanner interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}
type result interface{ RowsAffected() (int64, error) }
type txQueryer interface {
	QueryRowContext(context.Context, string, ...any) rowScanner
}

type sqlTxQueryer struct{ tx *sql.Tx }

func (q sqlTxQueryer) QueryRowContext(ctx context.Context, s string, a ...any) rowScanner {
	return q.tx.QueryRowContext(ctx, s, a...)
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) rowScanner
	QueryContext(context.Context, string, ...any) (rowsScanner, error)
	ExecContext(context.Context, string, ...any) (result, error)
}
type transactor interface {
	readWrite(context.Context, func(queryer) error) error
}

type sqlTransactor struct{ database *sql.DB }

func (transactions sqlTransactor) readWrite(ctx context.Context, operation func(queryer) error) error {
	tx, err := transactions.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	wrapped := sqlQueries{tx: tx}
	if err := operation(wrapped); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

type sqlQueries struct{ tx *sql.Tx }

func (q sqlQueries) QueryRowContext(ctx context.Context, s string, a ...any) rowScanner {
	return q.tx.QueryRowContext(ctx, s, a...)
}
func (q sqlQueries) QueryContext(ctx context.Context, s string, a ...any) (rowsScanner, error) {
	return q.tx.QueryContext(ctx, s, a...)
}
func (q sqlQueries) ExecContext(ctx context.Context, s string, a ...any) (result, error) {
	return q.tx.ExecContext(ctx, s, a...)
}
