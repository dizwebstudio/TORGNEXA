package outboxrepo

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/outbox"
)

const (
	orgA = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	wsA  = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
)

func TestTransactionEnqueueRequiresExistingMatchingTenantScope(t *testing.T) {
	t.Parallel()
	event := validEvent(t)
	fake := &fakeTx{scopeOrg: orgA, scopeWS: wsA}
	enqueuer := newTransactionEnqueuer(fake)
	if err := enqueuer.Enqueue(context.Background(), event); err != nil {
		t.Fatalf("Enqueue() error=%v", err)
	}
	if fake.verifyCount != 1 || fake.insertCount != 1 {
		t.Fatalf("verify=%d insert=%d", fake.verifyCount, fake.insertCount)
	}
	if fake.insertEventID != event.ID || !strings.Contains(fake.envelope, `"event_id":"evt_outbox_008"`) {
		t.Fatalf("insert=%q envelope=%s", fake.insertEventID, fake.envelope)
	}

	mismatch := &fakeTx{scopeOrg: orgA, scopeWS: "018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002"}
	if err := newTransactionEnqueuer(mismatch).Enqueue(context.Background(), event); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("mismatch error=%v", err)
	}
	if mismatch.insertCount != 0 {
		t.Fatal("cross-tenant event reached INSERT")
	}
}

func TestTransactionEnqueueIsIdempotentOnlyForIdenticalEvent(t *testing.T) {
	t.Parallel()
	event := validEvent(t)
	same := &fakeTx{scopeOrg: orgA, scopeWS: wsA, duplicate: true, duplicateIdentical: true}
	if err := newTransactionEnqueuer(same).Enqueue(context.Background(), event); err != nil {
		t.Fatalf("same duplicate error=%v", err)
	}
	collision := &fakeTx{scopeOrg: orgA, scopeWS: wsA, duplicate: true, duplicateIdentical: false}
	if err := newTransactionEnqueuer(collision).Enqueue(context.Background(), event); !errors.Is(err, outbox.ErrInvalidRecord) {
		t.Fatalf("collision error=%v", err)
	}
}

func TestRepositoryClaimUsesTenantScopeSkipLockedLeaseAndStableEvent(t *testing.T) {
	t.Parallel()
	event := validEvent(t)
	envelope, _ := json.Marshal(event)
	until := time.Date(2026, 8, 9, 10, 1, 0, 0, time.UTC)
	q := &fakeQueries{claimRows: &fakeRows{rows: [][]any{{string(envelope), int64(1), "000102030405060708090a0b0c0d0e0f", "relay-1", until}}}}
	repo := newRepository(&fakeTransactor{queries: q}, bytes.NewReader([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}))
	claimed, err := repo.Claim(context.Background(), mustScope(t), "relay-1", 25, 30*time.Second)
	if err != nil {
		t.Fatalf("Claim() error=%v", err)
	}
	if len(claimed) != 1 || claimed[0].Event.ID != event.ID || claimed[0].Attempt != 1 {
		t.Fatalf("claimed=%#v", claimed)
	}
	compact := strings.ToUpper(strings.Join(strings.Fields(q.queryStatement), " "))
	for _, required := range []string{"FOR UPDATE SKIP LOCKED", "LEASE_TOKEN", "PUBLISHED_AT IS NULL", "EVENT_ENVELOPE IS NOT NULL"} {
		if !strings.Contains(compact, required) {
			t.Errorf("claim SQL missing %q", required)
		}
	}
	if q.scopeOrg != orgA || q.scopeWS != wsA {
		t.Fatalf("scope=%q/%q", q.scopeOrg, q.scopeWS)
	}
}

func TestRepositoryAckAndRetryAreCompareByLease(t *testing.T) {
	t.Parallel()
	q := &fakeQueries{rowsAffected: 1}
	repo := newRepository(&fakeTransactor{queries: q}, bytes.NewReader(make([]byte, 32)))
	scope := mustScope(t)
	at, _ := domain.NewUTCInstant(time.Date(2026, 8, 9, 10, 2, 0, 0, time.UTC))
	token := "00112233445566778899aabbccddeeff"
	if err := repo.MarkPublished(context.Background(), scope, "evt_outbox_008", token, at); err != nil {
		t.Fatalf("MarkPublished=%v", err)
	}
	if !strings.Contains(q.execStatement, "lease_token = $4") || !strings.Contains(q.execStatement, "lease_expires_at > clock_timestamp()") {
		t.Fatalf("ack SQL not lease guarded: %s", q.execStatement)
	}
	if err := repo.ReleaseForRetry(context.Background(), scope, "evt_outbox_008", token, at, "publish_failed"); err != nil {
		t.Fatalf("ReleaseForRetry=%v", err)
	}
	if !strings.Contains(q.execStatement, "last_error_code = $6") {
		t.Fatalf("retry SQL=%s", q.execStatement)
	}
	if err := repo.ReleaseForRetry(context.Background(), scope, "evt_outbox_008", token, at, "Bearer leaked"); !errors.Is(err, outbox.ErrInvalidRecord) {
		t.Fatalf("unsafe code error=%v", err)
	}

	q.rowsAffected = 0
	if err := repo.MarkPublished(context.Background(), scope, "evt_outbox_008", token, at); !errors.Is(err, outbox.ErrLeaseLost) {
		t.Fatalf("lost lease error=%v", err)
	}
}

func TestRepositoryClaimFailsClosedWhenLegacyUnpublishedRowsExist(t *testing.T) {
	t.Parallel()
	q := &fakeQueries{legacyPending: true}
	repo := newRepository(&fakeTransactor{queries: q}, bytes.NewReader(make([]byte, 16)))
	if _, err := repo.Claim(context.Background(), mustScope(t), "relay", 1, time.Second); !errors.Is(err, outbox.ErrLegacyRows) {
		t.Fatalf("legacy rows error=%v", err)
	}
	if q.queryStatement != "" {
		t.Fatal("relay claim ran despite legacy unpublished rows")
	}
}

func TestRepositoryConstructorAndTokenFailuresFailClosed(t *testing.T) {
	t.Parallel()
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) succeeded")
	}
	repo := newRepository(&fakeTransactor{queries: &fakeQueries{}}, bytes.NewReader(nil))
	if _, err := repo.Claim(context.Background(), mustScope(t), "relay", 1, time.Second); err == nil {
		t.Fatal("short random source accepted")
	}
	if _, err := repo.Claim(context.Background(), tenancy.Scope{}, "relay", 1, time.Second); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("scope error=%v", err)
	}
}

func validEvent(t *testing.T) eventbus.Event {
	t.Helper()
	instant, _ := domain.NewUTCInstant(time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC))
	typ, _ := eventbus.ParseEventType("commerce.orders.order_created.v1")
	return eventbus.Event{ID: "evt_outbox_008", Type: typ, OccurredAt: instant, OrganizationID: orgA, WorkspaceID: wsA, EntityType: "order", EntityID: "order_008", Source: "orders", CorrelationID: "corr_008", Data: json.RawMessage(`{"order_id":"order_008"}`)}
}
func mustScope(t *testing.T) tenancy.Scope {
	t.Helper()
	s, err := tenancy.ParseScope(orgA, wsA)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type fakeTx struct {
	scopeOrg, scopeWS             string
	verifyCount, insertCount      int
	insertEventID, envelope       string
	duplicate, duplicateIdentical bool
}

func (f *fakeTx) QueryRowContext(_ context.Context, statement string, args ...any) rowScanner {
	switch statement {
	case verifyTxScopeStatement:
		f.verifyCount++
		return fakeRow{values: []any{sql.NullString{String: f.scopeOrg, Valid: f.scopeOrg != ""}, sql.NullString{String: f.scopeWS, Valid: f.scopeWS != ""}}}
	case enqueueStatement:
		f.insertCount++
		f.insertEventID, _ = args[0].(string)
		f.envelope, _ = args[7].(string)
		if f.duplicate {
			return fakeRow{err: sql.ErrNoRows}
		}
		return fakeRow{values: []any{f.insertEventID}}
	case duplicateCheckStatement:
		return fakeRow{values: []any{f.duplicateIdentical}}
	default:
		return fakeRow{err: fmt.Errorf("unexpected statement %q", statement)}
	}
}

type fakeTransactor struct{ queries *fakeQueries }

func (f *fakeTransactor) readWrite(_ context.Context, operation func(queryer) error) error {
	return operation(f.queries)
}

type fakeQueries struct {
	scopeOrg, scopeWS             string
	legacyPending                 bool
	queryStatement, execStatement string
	claimRows                     *fakeRows
	rowsAffected                  int64
}

func (f *fakeQueries) QueryRowContext(_ context.Context, statement string, args ...any) rowScanner {
	switch statement {
	case applyScopeStatement:
		f.scopeOrg, _ = args[0].(string)
		f.scopeWS, _ = args[1].(string)
		return fakeRow{values: []any{f.scopeOrg, f.scopeWS}}
	case legacyPendingStatement:
		return fakeRow{values: []any{f.legacyPending}}
	default:
		return fakeRow{err: fmt.Errorf("unexpected row statement %q", statement)}
	}
}
func (f *fakeQueries) QueryContext(_ context.Context, statement string, _ ...any) (rowsScanner, error) {
	f.queryStatement = statement
	if f.claimRows == nil {
		return &fakeRows{}, nil
	}
	return f.claimRows, nil
}
func (f *fakeQueries) ExecContext(_ context.Context, statement string, _ ...any) (result, error) {
	f.execStatement = statement
	return fakeResult{rows: f.rowsAffected}, nil
}

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan %d != %d", len(dest), len(r.values))
	}
	for i, v := range r.values {
		if err := assign(dest[i], v); err != nil {
			return err
		}
	}
	return nil
}

type fakeRows struct {
	rows  [][]any
	index int
	err   error
}

func (r *fakeRows) Next() bool { return r.index < len(r.rows) }
func (r *fakeRows) Scan(dest ...any) error {
	if r.index >= len(r.rows) {
		return errors.New("no row")
	}
	vals := r.rows[r.index]
	r.index++
	if len(dest) != len(vals) {
		return errors.New("scan mismatch")
	}
	for i, v := range vals {
		if err := assign(dest[i], v); err != nil {
			return err
		}
	}
	return nil
}
func (r *fakeRows) Err() error   { return r.err }
func (r *fakeRows) Close() error { return nil }

type fakeResult struct{ rows int64 }

func (r fakeResult) RowsAffected() (int64, error) { return r.rows, nil }
func assign(dest any, value any) error {
	switch d := dest.(type) {
	case *string:
		v, ok := value.(string)
		if !ok {
			return fmt.Errorf("want string")
		}
		*d = v
	case *int64:
		v, ok := value.(int64)
		if !ok {
			return fmt.Errorf("want int64")
		}
		*d = v
	case *bool:
		v, ok := value.(bool)
		if !ok {
			return fmt.Errorf("want bool")
		}
		*d = v
	case *time.Time:
		v, ok := value.(time.Time)
		if !ok {
			return fmt.Errorf("want time")
		}
		*d = v
	case *sql.NullString:
		v, ok := value.(sql.NullString)
		if !ok {
			return fmt.Errorf("want NullString")
		}
		*d = v
	default:
		return fmt.Errorf("unsupported %T", dest)
	}
	return nil
}
