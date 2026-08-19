package inboxrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/inbox"
)

const (
	testOrg = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	testWS  = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
)

func TestProcessTransactionExecutesOnceAndWritesReceiptLast(t *testing.T) {
	t.Parallel()
	delivery := validDelivery(t)
	queries := &fakeQueries{scopeOrg: testOrg, scopeWS: testWS, receiptErr: sql.ErrNoRows}
	calls := 0
	result, err := processTransaction(context.Background(), queries, mustScope(t), "orders.projector.v1", delivery, func(context.Context) error {
		calls++
		if queries.inserted {
			t.Fatal("receipt inserted before business side effect")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("processTransaction() error=%v", err)
	}
	if result != inbox.ResultProcessed || calls != 1 || !queries.inserted {
		t.Fatalf("result=%v calls=%d inserted=%v", result, calls, queries.inserted)
	}
	if queries.insertArgs[3] != delivery.Event.ID || queries.insertArgs[4] != delivery.Event.Type.String() || queries.insertArgs[7] != int(delivery.Attempt) {
		t.Fatalf("insert args=%#v", queries.insertArgs)
	}
	if queries.lockKey == 0 || !strings.Contains(strings.ToUpper(queries.lockStatement), "PG_ADVISORY_XACT_LOCK") {
		t.Fatalf("lock key=%d statement=%q", queries.lockKey, queries.lockStatement)
	}
}

func TestDuplicateDeliverySkipsBusinessHandler(t *testing.T) {
	t.Parallel()
	delivery := validDelivery(t)
	fingerprint, _ := inbox.Fingerprint(delivery.Event)
	queries := &fakeQueries{scopeOrg: testOrg, scopeWS: testWS, receiptType: delivery.Event.Type.String(), receiptFingerprint: fingerprint}
	calls := 0
	result, err := processTransaction(context.Background(), queries, mustScope(t), "orders.projector.v1", delivery, func(context.Context) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("processTransaction() error=%v", err)
	}
	if result != inbox.ResultDuplicate || calls != 0 || queries.inserted {
		t.Fatalf("result=%v calls=%d inserted=%v", result, calls, queries.inserted)
	}
}

func TestEventIDCollisionFailsClosedBeforeHandler(t *testing.T) {
	t.Parallel()
	delivery := validDelivery(t)
	queries := &fakeQueries{scopeOrg: testOrg, scopeWS: testWS, receiptType: delivery.Event.Type.String(), receiptFingerprint: strings.Repeat("0", 64)}
	calls := 0
	_, err := processTransaction(context.Background(), queries, mustScope(t), "orders.projector.v1", delivery, func(context.Context) error {
		calls++
		return nil
	})
	if !errors.Is(err, inbox.ErrCollision) || calls != 0 || queries.inserted {
		t.Fatalf("error=%v calls=%d inserted=%v", err, calls, queries.inserted)
	}
}

func TestCrashRetryLeavesNoReceiptUntilSuccessfulAttempt(t *testing.T) {
	t.Parallel()
	delivery := validDelivery(t)
	first := &fakeQueries{scopeOrg: testOrg, scopeWS: testWS, receiptErr: sql.ErrNoRows}
	failure := eventbus.Retryable("transient_db")
	if _, err := processTransaction(context.Background(), first, mustScope(t), "orders.projector.v1", delivery, func(context.Context) error {
		return failure
	}); !errors.Is(err, failure) {
		t.Fatalf("first attempt error=%v", err)
	}
	if first.inserted {
		t.Fatal("failed handler wrote an inbox receipt")
	}

	second := &fakeQueries{scopeOrg: testOrg, scopeWS: testWS, receiptErr: sql.ErrNoRows}
	calls := 0
	result, err := processTransaction(context.Background(), second, mustScope(t), "orders.projector.v1", delivery, func(context.Context) error {
		calls++
		return nil
	})
	if err != nil || result != inbox.ResultProcessed || calls != 1 || !second.inserted {
		t.Fatalf("retry result=%v error=%v calls=%d inserted=%v", result, err, calls, second.inserted)
	}
}

func TestCrossTenantDeliveryRejectedBeforeTransactionWork(t *testing.T) {
	t.Parallel()
	delivery := validDelivery(t)
	delivery.Event.WorkspaceID = "018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002"
	if err := validateInput(mustScope(t), "orders.projector.v1", delivery); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("validateInput() error=%v", err)
	}
}

func TestLockKeyNamespacesTenantConsumerAndEvent(t *testing.T) {
	t.Parallel()
	scope := mustScope(t)
	one := idempotencyLockKey(scope, "orders.projector.v1", "evt_1")
	two := idempotencyLockKey(scope, "orders.projector.v2", "evt_1")
	three := idempotencyLockKey(scope, "orders.projector.v1", "evt_2")
	if one == two || one == three || two == three {
		t.Fatalf("lock keys collided in deterministic test: %d %d %d", one, two, three)
	}
}

func TestReceiptQueryFailureDoesNotInvokeHandler(t *testing.T) {
	t.Parallel()
	queries := &fakeQueries{scopeOrg: testOrg, scopeWS: testWS, receiptErr: errors.New("database unavailable")}
	calls := 0
	if _, err := processTransaction(context.Background(), queries, mustScope(t), "orders.projector.v1", validDelivery(t), func(context.Context) error {
		calls++
		return nil
	}); err == nil || calls != 0 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func validDelivery(t *testing.T) eventbus.Delivery {
	t.Helper()
	occurred, err := domain.NewUTCInstant(time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	first, err := domain.NewUTCInstant(time.Date(2026, 8, 9, 10, 0, 1, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	typeID, err := eventbus.ParseEventType("commerce.orders.order_created.v1")
	if err != nil {
		t.Fatal(err)
	}
	return eventbus.Delivery{Event: eventbus.Event{
		ID:             "evt_inbox_009",
		Type:           typeID,
		OccurredAt:     occurred,
		OrganizationID: testOrg,
		WorkspaceID:    testWS,
		EntityType:     "order",
		EntityID:       "order_001",
		Source:         "orders",
		CorrelationID:  "corr_001",
		Data:           json.RawMessage(`{"order_id":"order_001"}`),
	}, Attempt: 2, FirstObservedAt: first}
}

func mustScope(t *testing.T) tenancy.Scope {
	t.Helper()
	scope, err := tenancy.ParseScope(testOrg, testWS)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

type fakeQueries struct {
	scopeOrg, scopeWS               string
	receiptType, receiptFingerprint string
	receiptErr                      error
	inserted                        bool
	insertArgs                      []any
	lockKey                         int64
	lockStatement                   string
}

func (queries *fakeQueries) QueryRowContext(_ context.Context, statement string, args ...any) rowScanner {
	switch statement {
	case applyScopeStatement:
		return fakeRow{values: []any{queries.scopeOrg, queries.scopeWS}}
	case advisoryLockStatement:
		queries.lockStatement = statement
		queries.lockKey = args[0].(int64)
		return fakeRow{values: []any{queries.lockKey}}
	case receiptStatement:
		if queries.receiptErr != nil {
			return fakeRow{err: queries.receiptErr}
		}
		return fakeRow{values: []any{queries.receiptType, queries.receiptFingerprint}}
	default:
		return fakeRow{err: errors.New("unexpected query")}
	}
}

func (queries *fakeQueries) ExecContext(_ context.Context, statement string, args ...any) (result, error) {
	if statement != insertReceiptStatement {
		return nil, errors.New("unexpected exec")
	}
	queries.inserted = true
	queries.insertArgs = append([]any(nil), args...)
	return fakeResult(1), nil
}

type fakeRow struct {
	values []any
	err    error
}

func (row fakeRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != len(row.values) {
		return errors.New("scan arity mismatch")
	}
	for i, value := range row.values {
		switch target := dest[i].(type) {
		case *string:
			*target = value.(string)
		case *int64:
			*target = value.(int64)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}

type fakeResult int64

func (result fakeResult) RowsAffected() (int64, error) { return int64(result), nil }
