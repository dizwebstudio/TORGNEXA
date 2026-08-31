// Package inboxrepo implements the PostgreSQL consumer inbox/idempotency boundary.
package inboxrepo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/inbox"
)

const applyScopeStatement = `SELECT
  set_config('app.organization_id', $1, true),
  set_config('app.workspace_id', $2, true)`

const advisoryLockStatement = `WITH locked AS (SELECT pg_advisory_xact_lock($1)) SELECT $1 FROM locked`

const receiptStatement = `SELECT event_type, event_fingerprint
FROM inbox_receipts
WHERE organization_id = $1
  AND workspace_id = $2
  AND consumer = $3
  AND event_id = $4`

const insertReceiptStatement = `INSERT INTO inbox_receipts (
  organization_id, workspace_id, consumer, event_id, event_type,
  event_fingerprint, first_observed_at, processed_at, processed_attempt
) VALUES ($1,$2,$3,$4,$5,$6,$7,clock_timestamp(),$8)`

// Transaction is the intentionally commit-less SQL surface given to a consumer
// handler. A handler can perform reads/writes in the inbox-owned transaction but
// cannot commit or roll it back through this interface.
type Transaction interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Handler performs all externally visible PostgreSQL side effects for one
// delivery. Returning an error rolls the transaction back, including all
// handler writes. It must not start an independent transaction for side effects
// that are expected to be covered by inbox idempotency.
type Handler func(context.Context, Transaction, eventbus.Delivery) error

// SQLHandler is the transaction-aware form of Handler. It is used when an
// inbound delivery must atomically write both its inbox receipt and a
// transactional-outbox event. The callback must not commit or roll back tx.
type SQLHandler func(context.Context, *sql.Tx, eventbus.Delivery) error

// Processor owns the PostgreSQL transaction containing duplicate check,
// business side effects and the final immutable receipt insert.
type Processor struct{ database *sql.DB }

func New(database *sql.DB) (*Processor, error) {
	if database == nil {
		return nil, errors.New("inbox processor: database is required")
	}
	return &Processor{database: database}, nil
}

// Process executes handler exactly once per committed tenant/consumer/event ID
// and immutable event fingerprint. A crash before commit leaves no receipt and
// no committed side effect; a crash after commit makes redelivery a duplicate.
func (processor *Processor) Process(ctx context.Context, scope tenancy.Scope, consumer string, delivery eventbus.Delivery, handler Handler) (inbox.Result, error) {
	if handler == nil {
		return 0, errors.New("inbox processor: handler is required")
	}
	return processor.process(ctx, scope, consumer, delivery, func(callCtx context.Context, tx *sql.Tx, item eventbus.Delivery) error {
		return handler(callCtx, sqlTransaction{tx: tx}, item)
	})
}

// ProcessWithSQLTransaction executes a transaction-aware handler exactly once
// and exposes the caller-owned SQL transaction to it. This keeps an inbound
// provider webhook's inbox receipt and outbox publication in the same commit.
func (processor *Processor) ProcessWithSQLTransaction(ctx context.Context, scope tenancy.Scope, consumer string, delivery eventbus.Delivery, handler SQLHandler) (inbox.Result, error) {
	if handler == nil {
		return 0, errors.New("inbox processor: SQL handler is required")
	}
	return processor.process(ctx, scope, consumer, delivery, handler)
}

func (processor *Processor) process(ctx context.Context, scope tenancy.Scope, consumer string, delivery eventbus.Delivery, handler SQLHandler) (inbox.Result, error) {
	if ctx == nil {
		return 0, errors.New("inbox processor: context is required")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if processor == nil || processor.database == nil {
		return 0, errors.New("inbox processor: processor is not initialized")
	}
	if err := validateInput(scope, consumer, delivery); err != nil {
		return 0, err
	}

	tx, err := processor.database.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("inbox processor: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	result, err := processTransaction(ctx, sqlQueries{tx: tx}, scope, consumer, delivery, func(callCtx context.Context) error {
		return handler(callCtx, tx, delivery)
	})
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("inbox processor: commit transaction: %w", err)
	}
	committed = true
	return result, nil
}

// EventHandler adapts the processor to Task-007 EventBus. A duplicate is a
// successful handler outcome so Kafka can commit the source offset. Deterministic
// event-ID collisions are permanent poison data; infrastructure/DB failures stay
// retryable through EventBus's default unknown-error classification.
func (processor *Processor) EventHandler(scope tenancy.Scope, consumer string, handler Handler) eventbus.Handler {
	return func(ctx context.Context, delivery eventbus.Delivery) error {
		_, err := processor.Process(ctx, scope, consumer, delivery, handler)
		if errors.Is(err, inbox.ErrCollision) {
			return eventbus.Permanent("inbox_event_collision")
		}
		if errors.Is(err, inbox.ErrInvalidRecord) || errors.Is(err, tenancy.ErrInvalidScope) {
			return eventbus.Permanent("inbox_invalid_record")
		}
		return err
	}
}

func validateInput(scope tenancy.Scope, consumer string, delivery eventbus.Delivery) error {
	if !scope.Valid() {
		return tenancy.ErrInvalidScope
	}
	if err := inbox.ValidateConsumer(consumer); err != nil {
		return err
	}
	if err := delivery.Validate(); err != nil {
		return inbox.ErrInvalidRecord
	}
	if delivery.Event.OrganizationID != scope.OrganizationID().String() || delivery.Event.WorkspaceID != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	return nil
}

type transactionOperation func(context.Context) error

func processTransaction(ctx context.Context, queries queryer, scope tenancy.Scope, consumer string, delivery eventbus.Delivery, operation transactionOperation) (inbox.Result, error) {
	if queries == nil || operation == nil {
		return 0, errors.New("inbox processor: transaction operation is not initialized")
	}
	fingerprint, err := inbox.Fingerprint(delivery.Event)
	if err != nil {
		return 0, err
	}
	if err := applyScope(ctx, queries, scope); err != nil {
		return 0, err
	}
	lockKey := idempotencyLockKey(scope, consumer, delivery.Event.ID)
	var locked int64
	if err := queries.QueryRowContext(ctx, advisoryLockStatement, lockKey).Scan(&locked); err != nil {
		return 0, fmt.Errorf("inbox processor: acquire transaction lock: %w", err)
	}
	if locked != lockKey {
		return 0, errors.New("inbox processor: advisory lock acknowledgement mismatch")
	}

	var eventType, existingFingerprint string
	err = queries.QueryRowContext(ctx, receiptStatement,
		scope.OrganizationID().String(), scope.WorkspaceID().String(), consumer, delivery.Event.ID,
	).Scan(&eventType, &existingFingerprint)
	switch {
	case err == nil:
		if eventType != delivery.Event.Type.String() || existingFingerprint != fingerprint {
			return 0, inbox.ErrCollision
		}
		return inbox.ResultDuplicate, nil
	case !errors.Is(err, sql.ErrNoRows):
		return 0, fmt.Errorf("inbox processor: inspect receipt: %w", err)
	}

	if err := operation(ctx); err != nil {
		return 0, err
	}
	if _, err := queries.ExecContext(ctx, insertReceiptStatement,
		scope.OrganizationID().String(), scope.WorkspaceID().String(), consumer,
		delivery.Event.ID, delivery.Event.Type.String(), fingerprint,
		delivery.FirstObservedAt.Time(), int(delivery.Attempt),
	); err != nil {
		return 0, fmt.Errorf("inbox processor: insert receipt: %w", err)
	}
	return inbox.ResultProcessed, nil
}

func applyScope(ctx context.Context, queries queryer, scope tenancy.Scope) error {
	var organizationID, workspaceID string
	if err := queries.QueryRowContext(ctx, applyScopeStatement, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&organizationID, &workspaceID); err != nil {
		return fmt.Errorf("inbox processor: apply tenant scope: %w", err)
	}
	if organizationID != scope.OrganizationID().String() || workspaceID != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	return nil
}

func idempotencyLockKey(scope tenancy.Scope, consumer, eventID string) int64 {
	fingerprint, _ := inboxKeyDigest(scope, consumer, eventID)
	return int64(binary.BigEndian.Uint64(fingerprint[:8]))
}

func inboxKeyDigest(scope tenancy.Scope, consumer, eventID string) ([32]byte, error) {
	// Length-prefixing makes the tuple unambiguous without relying on delimiters.
	payload := make([]byte, 0, 512)
	for _, value := range []string{scope.OrganizationID().String(), scope.WorkspaceID().String(), consumer, eventID} {
		if len(value) > 65535 {
			return [32]byte{}, inbox.ErrInvalidRecord
		}
		payload = append(payload, byte(len(value)>>8), byte(len(value)))
		payload = append(payload, value...)
	}
	return sha256Sum(payload), nil
}

func sha256Sum(data []byte) [32]byte {
	// Small wrapper keeps lock-key construction private and deterministic.
	return sha256Digest(data)
}

// split out for tests/tooling without exposing an alternate idempotency key.
var sha256Digest = func(data []byte) [32]byte {
	return sha256.Sum256(data)
}

type rowScanner interface{ Scan(...any) error }
type queryer interface {
	QueryRowContext(context.Context, string, ...any) rowScanner
	ExecContext(context.Context, string, ...any) (result, error)
}
type result interface{ RowsAffected() (int64, error) }

type sqlQueries struct{ tx *sql.Tx }

type sqlTransaction struct{ tx *sql.Tx }

func (transaction sqlTransaction) ExecContext(ctx context.Context, statement string, arguments ...any) (sql.Result, error) {
	return transaction.tx.ExecContext(ctx, statement, arguments...)
}

func (transaction sqlTransaction) QueryContext(ctx context.Context, statement string, arguments ...any) (*sql.Rows, error) {
	return transaction.tx.QueryContext(ctx, statement, arguments...)
}

func (transaction sqlTransaction) QueryRowContext(ctx context.Context, statement string, arguments ...any) *sql.Row {
	return transaction.tx.QueryRowContext(ctx, statement, arguments...)
}

func (queries sqlQueries) QueryRowContext(ctx context.Context, statement string, arguments ...any) rowScanner {
	return queries.tx.QueryRowContext(ctx, statement, arguments...)
}
func (queries sqlQueries) ExecContext(ctx context.Context, statement string, arguments ...any) (result, error) {
	return queries.tx.ExecContext(ctx, statement, arguments...)
}
