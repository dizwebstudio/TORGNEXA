package backfillrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/migration"
)

const (
	jobID        = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0200"
	organization = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	workspace    = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
)

func TestAcquireRegistersAndClaimsTenantLease(t *testing.T) {
	t.Parallel()
	job := tenantJob(t)
	queries := newFakeQueries(job)
	repository := newRepository(&fakeTransactor{queries: queries})
	lease, acquired, err := repository.Acquire(context.Background(), job, "worker-1", time.Minute)
	if err != nil || !acquired {
		t.Fatalf("Acquire() = %#v, %v, %v", lease, acquired, err)
	}
	if lease.Job != job || lease.WorkerID != "worker-1" || lease.Generation != 1 || lease.Checkpoint != "cursor-10" {
		t.Fatalf("lease = %#v", lease)
	}
	if len(queries.execCalls) != 1 || queries.execCalls[0].statement != ensureJobStatement {
		t.Fatalf("registration calls = %#v", queries.execCalls)
	}
	wantScope := []any{job.ID, job.Key, organization, workspace, job.BatchSize}
	for index, value := range wantScope {
		if queries.execCalls[0].arguments[index] != value {
			t.Errorf("registration argument %d = %#v, want %#v", index, queries.execCalls[0].arguments[index], value)
		}
	}
	if len(queries.queryCalls) != 2 || queries.queryCalls[1].statement != claimStatement {
		t.Fatalf("query calls = %#v", queries.queryCalls)
	}
	claimArguments := queries.queryCalls[1].arguments
	if claimArguments[2] != organization || claimArguments[3] != workspace || claimArguments[5] != "worker-1" || claimArguments[6] != int64(60000) {
		t.Fatalf("claim arguments = %#v", claimArguments)
	}
}

func TestAcquireGlobalNoWorkAndMetadataMismatch(t *testing.T) {
	t.Parallel()
	global := migration.BackfillJob{ID: jobID, Key: "synthetic.global", Scope: migration.GlobalBackfillScope(), BatchSize: 50}
	queries := newFakeQueries(global)
	queries.claimErr = sql.ErrNoRows
	repository := newRepository(&fakeTransactor{queries: queries})
	lease, acquired, err := repository.Acquire(context.Background(), global, "worker-global", time.Minute)
	if err != nil || acquired || lease != (migration.Lease{}) {
		t.Fatalf("global Acquire() = %#v, %v, %v", lease, acquired, err)
	}
	if queries.execCalls[0].arguments[2] != nil || queries.execCalls[0].arguments[3] != nil {
		t.Fatalf("global scope arguments = %#v", queries.execCalls[0].arguments)
	}

	mismatchQueries := newFakeQueries(global)
	mismatchQueries.metadataKey = "synthetic.different"
	_, _, err = newRepository(&fakeTransactor{queries: mismatchQueries}).Acquire(context.Background(), global, "worker-global", time.Minute)
	if !errors.Is(err, migration.ErrInvalidBackfill) || len(mismatchQueries.queryCalls) != 1 {
		t.Fatalf("metadata mismatch error/calls = %v/%d", err, len(mismatchQueries.queryCalls))
	}
}

func TestCommitAndFailFenceStaleWorkers(t *testing.T) {
	t.Parallel()
	job := tenantJob(t)
	lease := migration.Lease{Job: job, WorkerID: "worker-1", Generation: 3, Checkpoint: "cursor-10"}

	commitQueries := newFakeQueries(job)
	repository := newRepository(&fakeTransactor{queries: commitQueries})
	result := migration.BatchResult{NextCheckpoint: "cursor-20", Processed: 10, Done: false}
	if err := repository.Commit(context.Background(), lease, result); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	call := commitQueries.execCalls[0]
	if call.statement != commitStatement || call.arguments[5] != "worker-1" || call.arguments[6] != int64(3) || call.arguments[7] != "cursor-20" || call.arguments[8] != 10 || call.arguments[9] != false {
		t.Fatalf("commit call = %#v", call)
	}

	staleQueries := newFakeQueries(job)
	staleQueries.affectedRows = 0
	err := newRepository(&fakeTransactor{queries: staleQueries}).Commit(context.Background(), lease, result)
	if !errors.Is(err, migration.ErrLeaseLost) {
		t.Fatalf("stale Commit() error = %v", err)
	}

	failQueries := newFakeQueries(job)
	if err := newRepository(&fakeTransactor{queries: failQueries}).Fail(context.Background(), lease, migration.FailureProcessor); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	failCall := failQueries.execCalls[0]
	if failCall.statement != failStatement || failCall.arguments[7] != "processor_failed" {
		t.Fatalf("failure call = %#v", failCall)
	}
	if err := newRepository(&fakeTransactor{queries: newFakeQueries(job)}).Fail(context.Background(), lease, migration.FailureCode("raw_secret")); !errors.Is(err, migration.ErrInvalidBackfill) {
		t.Fatalf("unsafe failure code error = %v", err)
	}
}

func TestRepositoryFailsClosedBeforeDatabaseWork(t *testing.T) {
	t.Parallel()
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) succeeded")
	}
	job := tenantJob(t)
	queries := newFakeQueries(job)
	transactions := &fakeTransactor{queries: queries}
	repository := newRepository(transactions)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := repository.Acquire(canceled, job, "worker-1", time.Minute); !errors.Is(err, context.Canceled) || transactions.count != 0 {
		t.Fatalf("canceled Acquire() error/transactions = %v/%d", err, transactions.count)
	}
	invalid := job
	invalid.BatchSize = 0
	if _, _, err := repository.Acquire(context.Background(), invalid, "worker-1", time.Minute); !errors.Is(err, migration.ErrInvalidBackfill) || transactions.count != 0 {
		t.Fatalf("invalid Acquire() error/transactions = %v/%d", err, transactions.count)
	}
}

func TestSQLCarriesScopeExpiryAndFencingPredicates(t *testing.T) {
	t.Parallel()
	for _, predicate := range []string{
		"organization_id IS NOT DISTINCT FROM $3",
		"workspace_id IS NOT DISTINCT FROM $4",
		"lease_until <= clock_timestamp()",
		"lease_generation = lease_generation + 1",
	} {
		if !strings.Contains(claimStatement, predicate) {
			t.Errorf("claim SQL is missing %q", predicate)
		}
	}
	for _, statement := range []string{commitStatement, failStatement} {
		for _, predicate := range []string{"lease_owner = $6", "lease_generation = $7", "organization_id IS NOT DISTINCT FROM $3", "workspace_id IS NOT DISTINCT FROM $4"} {
			if !strings.Contains(statement, predicate) {
				t.Errorf("fenced SQL is missing %q", predicate)
			}
		}
		if !strings.Contains(statement, "lease_until > clock_timestamp()") {
			t.Error("fenced SQL does not reject an expired lease")
		}
	}
}

type fakeTransactor struct {
	queries *fakeQueries
	err     error
	count   int
}

func (transactions *fakeTransactor) within(_ context.Context, operation func(queryer) error) error {
	transactions.count++
	if transactions.err != nil {
		return transactions.err
	}
	return operation(transactions.queries)
}

type queryCall struct {
	statement string
	arguments []any
}

type fakeQueries struct {
	job               migration.BackfillJob
	metadataKey       string
	metadataOrg       sql.NullString
	metadataWorkspace sql.NullString
	metadataBatch     int
	claimCheckpoint   string
	claimGeneration   int64
	claimErr          error
	execErr           error
	affectedRows      int64
	execCalls         []queryCall
	queryCalls        []queryCall
}

func newFakeQueries(job migration.BackfillJob) *fakeQueries {
	organizationValue, workspaceValue := scopeNullStrings(job.Scope)
	return &fakeQueries{
		job:               job,
		metadataKey:       job.Key,
		metadataOrg:       organizationValue,
		metadataWorkspace: workspaceValue,
		metadataBatch:     job.BatchSize,
		claimCheckpoint:   "cursor-10",
		claimGeneration:   1,
		affectedRows:      1,
	}
}

func (queries *fakeQueries) ExecContext(_ context.Context, statement string, arguments ...any) (sql.Result, error) {
	queries.execCalls = append(queries.execCalls, queryCall{statement: statement, arguments: append([]any(nil), arguments...)})
	if queries.execErr != nil {
		return nil, queries.execErr
	}
	return fakeResult{rows: queries.affectedRows}, nil
}

func (queries *fakeQueries) QueryRowContext(_ context.Context, statement string, arguments ...any) rowScanner {
	queries.queryCalls = append(queries.queryCalls, queryCall{statement: statement, arguments: append([]any(nil), arguments...)})
	switch statement {
	case jobMetadataQuery:
		return fakeRow{values: []any{queries.metadataKey, queries.metadataOrg, queries.metadataWorkspace, queries.metadataBatch}}
	case claimStatement:
		return fakeRow{values: []any{queries.claimCheckpoint, queries.claimGeneration}, err: queries.claimErr}
	default:
		return fakeRow{err: errors.New("unexpected query")}
	}
}

type fakeRow struct {
	values []any
	err    error
}

func (row fakeRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != len(row.values) {
		return fmt.Errorf("destination count %d, want %d", len(destinations), len(row.values))
	}
	for index, destination := range destinations {
		switch typed := destination.(type) {
		case *string:
			value, ok := row.values[index].(string)
			if !ok {
				return errors.New("value is not string")
			}
			*typed = value
		case *int:
			value, ok := row.values[index].(int)
			if !ok {
				return errors.New("value is not int")
			}
			*typed = value
		case *int64:
			value, ok := row.values[index].(int64)
			if !ok {
				return errors.New("value is not int64")
			}
			*typed = value
		case *sql.NullString:
			value, ok := row.values[index].(sql.NullString)
			if !ok {
				return errors.New("value is not NullString")
			}
			*typed = value
		default:
			return fmt.Errorf("unsupported destination %T", destination)
		}
	}
	return nil
}

type fakeResult struct{ rows int64 }

func (result fakeResult) LastInsertId() (int64, error) { return 0, errors.New("not supported") }
func (result fakeResult) RowsAffected() (int64, error) { return result.rows, nil }

func tenantJob(t *testing.T) migration.BackfillJob {
	t.Helper()
	scope, err := tenancy.ParseScope(organization, workspace)
	if err != nil {
		t.Fatal(err)
	}
	backfillScope, err := migration.WorkspaceBackfillScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	return migration.BackfillJob{ID: jobID, Key: "synthetic.tenant", Scope: backfillScope, BatchSize: 100}
}

func scopeNullStrings(scope migration.BackfillScope) (sql.NullString, sql.NullString) {
	if scope.Global() {
		return sql.NullString{}, sql.NullString{}
	}
	return sql.NullString{String: scope.OrganizationID().String(), Valid: true}, sql.NullString{String: scope.WorkspaceID().String(), Valid: true}
}
