package auditrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
)

const (
	organizationA = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	workspaceA    = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
	auditA        = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003"
)

func TestRepositoryAppendAppliesTenantScopeAndWritesSafeRecord(t *testing.T) {
	t.Parallel()
	queries := &fakeQueries{}
	transactions := &fakeTransactor{queries: queries}
	repository := newRepository(transactions)
	scope := mustScope(t, organizationA, workspaceA)
	record := validRecord(t, scope)

	if err := repository.Append(context.Background(), scope, record); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if transactions.count != 1 || queries.scopeCount != 1 || queries.execCount != 1 {
		t.Fatalf("transactions=%d scope=%d exec=%d", transactions.count, queries.scopeCount, queries.execCount)
	}
	if queries.scopeOrganization != organizationA || queries.scopeWorkspace != workspaceA {
		t.Fatalf("scope = %q/%q", queries.scopeOrganization, queries.scopeWorkspace)
	}
	if queries.statement != appendStatement || len(queries.arguments) != 12 {
		t.Fatalf("statement/args = %q %#v", queries.statement, queries.arguments)
	}
	if queries.arguments[1] != organizationA || queries.arguments[2] != workspaceA || queries.arguments[9] != "write_sensitive" {
		t.Fatalf("tenant/risk args = %#v", queries.arguments)
	}
	summary, ok := queries.arguments[10].(string)
	if !ok || strings.Contains(strings.ToLower(summary), "bearer") || !strings.Contains(summary, "changed_fields") {
		t.Fatalf("summary argument = %#v", queries.arguments[10])
	}
}

func TestRepositoryRejectsCrossTenantAndUnsanitizedRecordsBeforeTransaction(t *testing.T) {
	t.Parallel()
	queries := &fakeQueries{}
	transactions := &fakeTransactor{queries: queries}
	repository := newRepository(transactions)
	scope := mustScope(t, organizationA, workspaceA)
	record := validRecord(t, scope)

	otherWorkspace, err := tenancy.ParseWorkspaceID("018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002")
	if err != nil {
		t.Fatal(err)
	}
	record.WorkspaceID = otherWorkspace
	if err := repository.Append(context.Background(), scope, record); !errors.Is(err, audit.ErrInvalidRecord) {
		t.Fatalf("cross-tenant Append() error = %v", err)
	}
	if transactions.count != 0 {
		t.Fatalf("cross-tenant record began %d transactions", transactions.count)
	}

	record = validRecord(t, scope)
	record.Summary = audit.Summary{"authorization": "Bearer forbidden"}
	if err := repository.Append(context.Background(), scope, record); !errors.Is(err, audit.ErrInvalidRecord) {
		t.Fatalf("unsafe summary error = %v", err)
	}
	if transactions.count != 0 {
		t.Fatalf("unsafe record began %d transactions", transactions.count)
	}
}

func TestRepositoryFailsClosedOnScopeOrDatabaseErrors(t *testing.T) {
	t.Parallel()
	scope := mustScope(t, organizationA, workspaceA)
	record := validRecord(t, scope)

	queries := &fakeQueries{scopeErr: errors.New("synthetic scope failure")}
	repository := newRepository(&fakeTransactor{queries: queries})
	if err := repository.Append(context.Background(), scope, record); err == nil || !strings.Contains(err.Error(), "apply tenant scope") {
		t.Fatalf("scope failure error = %v", err)
	}
	if queries.execCount != 0 {
		t.Fatalf("append ran %d times after scope failure", queries.execCount)
	}

	queries = &fakeQueries{execErr: errors.New("synthetic insert failure")}
	repository = newRepository(&fakeTransactor{queries: queries})
	if err := repository.Append(context.Background(), scope, record); err == nil || !strings.Contains(err.Error(), "append audit record") {
		t.Fatalf("insert failure error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := repository.Append(canceled, scope, record); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Append() error = %v", err)
	}
	if err := repository.Append(context.Background(), tenancy.Scope{}, record); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("invalid scope error = %v", err)
	}
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) succeeded")
	}
}

func TestSQLIsInsertOnlyAndTenantScoped(t *testing.T) {
	t.Parallel()
	compact := strings.ToUpper(strings.Join(strings.Fields(appendStatement), " "))
	if !strings.HasPrefix(compact, "INSERT INTO AUDIT_RECORDS") {
		t.Fatalf("append SQL = %q", compact)
	}
	for _, forbidden := range []string{" UPDATE ", " DELETE ", " TRUNCATE ", " ON CONFLICT "} {
		if strings.Contains(" "+compact+" ", forbidden) {
			t.Errorf("append SQL contains forbidden %q", forbidden)
		}
	}
	if !strings.Contains(applyScopeStatement, "true)") {
		t.Fatal("tenant GUC scope must be transaction-local")
	}
	for _, column := range []string{"organization_id", "workspace_id", "risk", "summary", "created_at"} {
		if !strings.Contains(appendStatement, column) {
			t.Errorf("append SQL is missing %s", column)
		}
	}
}

func validRecord(t *testing.T, scope tenancy.Scope) audit.Record {
	t.Helper()
	summary, err := audit.SanitizeSummary(audit.Summary{
		"changed_fields": []any{"title", "price"},
		"authorization":  "Bearer forbidden",
	})
	if err != nil {
		t.Fatal(err)
	}
	return audit.Record{
		ID:             auditA,
		OrganizationID: scope.OrganizationID(),
		WorkspaceID:    scope.WorkspaceID(),
		ActorID:        "oidc:user-42",
		Source:         "api",
		Action:         "catalog.product.update",
		ResourceType:   "product",
		ResourceID:     "product-42",
		CorrelationID:  "request-42",
		Risk:           audit.RiskWriteSensitive,
		Summary:        summary,
		CreatedAt:      time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC),
	}
}

type fakeTransactor struct {
	queries *fakeQueries
	err     error
	count   int
}

func (transactions *fakeTransactor) readWrite(_ context.Context, operation func(queryer) error) error {
	transactions.count++
	if transactions.err != nil {
		return transactions.err
	}
	return operation(transactions.queries)
}

type fakeQueries struct {
	scopeOrganization string
	scopeWorkspace    string
	scopeCount        int
	execCount         int
	scopeErr          error
	execErr           error
	statement         string
	arguments         []any
}

func (queries *fakeQueries) QueryRowContext(_ context.Context, statement string, arguments ...any) rowScanner {
	if statement != applyScopeStatement {
		return fakeRow{err: errors.New("unexpected query")}
	}
	if queries.scopeErr != nil {
		return fakeRow{err: queries.scopeErr}
	}
	if len(arguments) != 2 {
		return fakeRow{err: errors.New("unexpected scope arguments")}
	}
	queries.scopeOrganization, _ = arguments[0].(string)
	queries.scopeWorkspace, _ = arguments[1].(string)
	queries.scopeCount++
	return fakeRow{values: []any{queries.scopeOrganization, queries.scopeWorkspace}}
}

func (queries *fakeQueries) ExecContext(_ context.Context, statement string, arguments ...any) (result, error) {
	queries.execCount++
	queries.statement = statement
	queries.arguments = append([]any(nil), arguments...)
	if queries.execErr != nil {
		return nil, queries.execErr
	}
	if len(arguments) < 3 || arguments[1] != queries.scopeOrganization || arguments[2] != queries.scopeWorkspace {
		return fakeResult{rows: 0}, nil
	}
	return fakeResult{rows: 1}, nil
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
		return fmt.Errorf("scan destination count %d, want %d", len(destinations), len(row.values))
	}
	for index, value := range row.values {
		destination, ok := destinations[index].(*string)
		if !ok {
			return fmt.Errorf("unsupported scan destination %T", destinations[index])
		}
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("value %d is not string", index)
		}
		*destination = text
	}
	return nil
}

type fakeResult struct {
	rows int64
	err  error
}

func (value fakeResult) RowsAffected() (int64, error) { return value.rows, value.err }

func mustScope(t *testing.T, organizationID, workspaceID string) tenancy.Scope {
	t.Helper()
	scope, err := tenancy.ParseScope(organizationID, workspaceID)
	if err != nil {
		t.Fatalf("ParseScope() error = %v", err)
	}
	return scope
}
