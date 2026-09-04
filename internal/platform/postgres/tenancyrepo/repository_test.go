package tenancyrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const (
	organizationA = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	workspaceA    = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
	storeA        = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003"
	organizationB = "018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001"
	workspaceB    = "018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002"
	missingStore  = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a9999"
)

func TestRepositoryReturnsScopedHierarchy(t *testing.T) {
	t.Parallel()
	queries := newFakeQueries()
	transactions := &fakeTransactor{queries: queries}
	repository := newRepository(transactions)
	scope := mustScope(t, organizationA, workspaceA)

	organization, err := repository.Organization(context.Background(), scope)
	if err != nil || organization.ID.String() != organizationA {
		t.Fatalf("Organization() = %#v, %v", organization, err)
	}
	workspace, err := repository.Workspace(context.Background(), scope)
	if err != nil || workspace.ID.String() != workspaceA || workspace.OrganizationID.String() != organizationA {
		t.Fatalf("Workspace() = %#v, %v", workspace, err)
	}
	storeID, _ := tenancy.ParseStoreID(storeA)
	store, err := repository.Store(context.Background(), scope, storeID)
	if err != nil || store.ID != storeID || store.OrganizationID.String() != organizationA || store.WorkspaceID.String() != workspaceA {
		t.Fatalf("Store() = %#v, %v", store, err)
	}
	if transactions.count != 3 || queries.scopeCount != 3 || queries.queryCount != 3 {
		t.Fatalf("transaction/scope/query counts = %d/%d/%d, want 3/3/3", transactions.count, queries.scopeCount, queries.queryCount)
	}
}

func TestRepositoryCrossTenantAndMissingAreIndistinguishable(t *testing.T) {
	t.Parallel()
	repository := newRepository(&fakeTransactor{queries: newFakeQueries()})
	storeID, _ := tenancy.ParseStoreID(storeA)
	_, crossTenantErr := repository.Store(context.Background(), mustScope(t, organizationB, workspaceB), storeID)
	if !errors.Is(crossTenantErr, tenancy.ErrNotFound) || strings.Contains(crossTenantErr.Error(), "Synthetic Store") {
		t.Fatalf("cross-tenant error = %v, want opaque ErrNotFound", crossTenantErr)
	}
	missingID, _ := tenancy.ParseStoreID(missingStore)
	_, missingErr := repository.Store(context.Background(), mustScope(t, organizationA, workspaceA), missingID)
	if !errors.Is(missingErr, tenancy.ErrNotFound) {
		t.Fatalf("missing error = %v, want ErrNotFound", missingErr)
	}
	if crossTenantErr.Error() != missingErr.Error() {
		t.Fatalf("cross-tenant error %q differs from missing error %q", crossTenantErr, missingErr)
	}
	_, mixedScopeErr := repository.Store(context.Background(), mustScope(t, organizationA, workspaceB), storeID)
	if !errors.Is(mixedScopeErr, tenancy.ErrNotFound) || mixedScopeErr.Error() != missingErr.Error() {
		t.Fatalf("mixed organization/workspace error = %v, want opaque ErrNotFound", mixedScopeErr)
	}
}

func TestRepositoryFailsClosedBeforeLookup(t *testing.T) {
	t.Parallel()
	queries := newFakeQueries()
	transactions := &fakeTransactor{queries: queries}
	repository := newRepository(transactions)
	storeID, _ := tenancy.ParseStoreID(storeA)
	if _, err := repository.Store(context.Background(), tenancy.Scope{}, storeID); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("zero scope error = %v, want ErrInvalidScope", err)
	}
	if transactions.count != 0 {
		t.Fatalf("invalid scope began %d transactions", transactions.count)
	}

	scopeErr := errors.New("synthetic scope failure")
	queries.scopeErr = scopeErr
	if _, err := repository.Store(context.Background(), mustScope(t, organizationA, workspaceA), storeID); !errors.Is(err, scopeErr) {
		t.Fatalf("scope application error = %v", err)
	}
	if queries.queryCount != 0 {
		t.Fatalf("lookup ran %d times after scope failure", queries.queryCount)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.Store(canceled, mustScope(t, organizationA, workspaceA), storeID); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lookup error = %v", err)
	}
}

func TestRepositoryRejectsCorruptPersistedScope(t *testing.T) {
	t.Parallel()
	queries := newFakeQueries()
	queries.storeValues[1] = organizationB
	repository := newRepository(&fakeTransactor{queries: queries})
	storeID, _ := tenancy.ParseStoreID(storeA)
	_, err := repository.Store(context.Background(), mustScope(t, organizationA, workspaceA), storeID)
	if !errors.Is(err, tenancy.ErrInvalidRecord) {
		t.Fatalf("corrupt row error = %v, want ErrInvalidRecord", err)
	}
}

func TestQueriesContainMandatoryTenantPredicates(t *testing.T) {
	t.Parallel()
	compactOrganization := strings.Join(strings.Fields(organizationQuery), " ")
	for _, predicate := range []string{"o.id = $1", "w.organization_id = $1", "w.id = $2"} {
		if !strings.Contains(compactOrganization, predicate) {
			t.Errorf("organization query does not contain %q", predicate)
		}
	}
	compactWorkspace := strings.Join(strings.Fields(workspaceQuery), " ")
	for _, predicate := range []string{"w.organization_id = $1", "w.id = $2"} {
		if !strings.Contains(compactWorkspace, predicate) {
			t.Errorf("workspace query does not contain %q", predicate)
		}
	}
	compactStore := strings.Join(strings.Fields(storeQuery), " ")
	for _, predicate := range []string{"s.organization_id = $1", "s.workspace_id = $2", "s.id = $3"} {
		if !strings.Contains(compactStore, predicate) {
			t.Errorf("store query does not contain %q", predicate)
		}
	}
	if !strings.Contains(applyScopeStatement, "true)") {
		t.Fatal("tenant GUC scope must be transaction-local")
	}
	compactUpdate := strings.Join(strings.Fields(updateProfileQuery), " ")
	for _, predicate := range []string{"WHERE id=$1 AND version=$4", "WHERE organization_id=$1 AND id=$2 AND version=$6", "EXISTS (SELECT 1 FROM updated_organization)"} {
		if !strings.Contains(compactUpdate, predicate) {
			t.Errorf("profile update does not contain %q", predicate)
		}
	}
	compactMemberLock := strings.Join(strings.Fields(workspaceMemberMutationLockStatement), " ")
	for _, predicate := range []string{"pg_advisory_xact_lock", "hashtextextended", "workspace-members:"} {
		if !strings.Contains(compactMemberLock, predicate) {
			t.Errorf("workspace member mutation lock does not contain %q", predicate)
		}
	}
	for _, predicate := range []string{"organization_id=$1", "workspace_id=$2", "role_code='admin'", "status='active'"} {
		if !strings.Contains(strings.Join(strings.Fields(workspaceMemberAdminCountQuery), " "), predicate) {
			t.Errorf("workspace member administrator count does not contain %q", predicate)
		}
	}
}

func TestUpdateMemberSerializesWorkspaceAdministration(t *testing.T) {
	t.Parallel()
	queries := newMemberMutationQueries()
	repository := newRepository(&memberMutationTransactor{queries: queries})

	member, err := repository.UpdateMember(context.Background(), mustScope(t, organizationA, workspaceA), "member-a", "viewer", "disabled", "member-update-1", 1)
	if err != nil {
		t.Fatalf("UpdateMember() error = %v", err)
	}
	if member.ID != "member-a" || member.Status != "disabled" {
		t.Fatalf("UpdateMember() = %#v, want disabled member-a", member)
	}
	want := []string{applyScopeStatement, workspaceMemberMutationLockStatement, workspaceMemberMutationDigestColumnQuery, workspaceMemberStateQuery, workspaceMemberAdminCountQuery, workspaceMemberUpdateQuery}
	if !equalStrings(queries.statements, want) {
		t.Fatalf("workspace member statements = %#v, want %#v", queries.statements, want)
	}
}

func TestUpdateMemberRejectsIdempotencyPayloadMismatch(t *testing.T) {
	t.Parallel()
	queries := newMemberMutationQueries()
	queries.mutationKey = "member-update-1"
	queries.mutationHash = strings.Repeat("a", 64)
	repository := newRepository(&memberMutationTransactor{queries: queries})

	_, err := repository.UpdateMember(context.Background(), mustScope(t, organizationA, workspaceA), "member-a", "viewer", "disabled", queries.mutationKey, 1)
	if !errors.Is(err, ErrMemberConflict) {
		t.Fatalf("UpdateMember() error = %v, want ErrMemberConflict", err)
	}
	if len(queries.statements) != 4 || queries.statements[1] != workspaceMemberMutationLockStatement || queries.statements[2] != workspaceMemberMutationDigestColumnQuery || queries.statements[3] != workspaceMemberStateQuery {
		t.Fatalf("mismatched replay executed unexpected statements: %#v", queries.statements)
	}
}

func TestUpdateMemberReplaysSameIdempotencyPayload(t *testing.T) {
	t.Parallel()
	queries := newMemberMutationQueries()
	queries.memberRole = "viewer"
	queries.memberStatus = "active"
	queries.mutationKey = "member-update-1"
	queries.mutationHash = memberMutationDigest("member-a", "viewer", "active", 1)
	queries.memberValues[5] = "active"
	repository := newRepository(&memberMutationTransactor{queries: queries})

	member, err := repository.UpdateMember(context.Background(), mustScope(t, organizationA, workspaceA), "member-a", "viewer", "active", queries.mutationKey, 1)
	if err != nil {
		t.Fatalf("UpdateMember() replay error = %v", err)
	}
	if member.Status != "active" {
		t.Fatalf("replayed member status = %q, want active", member.Status)
	}
	if len(queries.statements) != 5 || queries.statements[3] != workspaceMemberStateQuery || queries.statements[4] != workspaceMemberUpdateQuery {
		t.Fatalf("replay executed unexpected statements: %#v", queries.statements)
	}
}

func TestUpdateMemberSupportsExpandWindowBeforeDigestMigration(t *testing.T) {
	t.Parallel()
	queries := newMemberMutationQueries()
	queries.digestColumn = false
	repository := newRepository(&memberMutationTransactor{queries: queries})

	if _, err := repository.UpdateMember(context.Background(), mustScope(t, organizationA, workspaceA), "member-a", "viewer", "disabled", "member-update-1", 1); err != nil {
		t.Fatalf("UpdateMember() during expand window error = %v", err)
	}
	want := []string{applyScopeStatement, workspaceMemberMutationLockStatement, workspaceMemberMutationDigestColumnQuery, workspaceMemberLegacyStateQuery, workspaceMemberAdminCountQuery, workspaceMemberLegacyUpdateQuery}
	if !equalStrings(queries.statements, want) {
		t.Fatalf("expand-window statements = %#v, want %#v", queries.statements, want)
	}
}

func TestNewRejectsNilDatabase(t *testing.T) {
	t.Parallel()
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) succeeded")
	}
}

type fakeTransactor struct {
	queries *fakeQueries
	err     error
	count   int
}

func (transactions *fakeTransactor) run(ctx context.Context, _ bool, operation func(queryer) error) error {
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
	queryCount        int
	scopeErr          error
	organizationVals  []any
	workspaceValues   []any
	storeValues       []any
}

type memberMutationTransactor struct {
	queries *memberMutationQueries
}

func (transactions *memberMutationTransactor) run(ctx context.Context, _ bool, operation func(queryer) error) error {
	return operation(transactions.queries)
}

type memberMutationQueries struct {
	organization string
	workspace    string
	statements   []string
	memberRole   string
	memberStatus string
	mutationKey  string
	mutationHash string
	digestColumn bool
	memberValues []any
}

func newMemberMutationQueries() *memberMutationQueries {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	return &memberMutationQueries{
		memberRole:   "admin",
		memberStatus: "active",
		digestColumn: true,
		memberValues: []any{"member-a", "admin@example.test", "Admin A", "", "viewer", "disabled", "invite-a", int64(2), now, now},
	}
}

func (queries *memberMutationQueries) QueryRowContext(_ context.Context, statement string, arguments ...any) rowScanner {
	queries.statements = append(queries.statements, statement)
	if statement == applyScopeStatement {
		queries.organization, _ = arguments[0].(string)
		queries.workspace, _ = arguments[1].(string)
		return fakeRow{values: []any{queries.organization, queries.workspace}}
	}
	if statement == workspaceMemberMutationDigestColumnQuery {
		return fakeRow{values: []any{queries.digestColumn}}
	}
	if len(arguments) < 2 || arguments[0] != queries.organization || arguments[1] != queries.workspace {
		return fakeRow{err: sql.ErrNoRows}
	}
	switch statement {
	case workspaceMemberMutationLockStatement:
		return fakeRow{values: []any{queries.organization + ":" + queries.workspace}}
	case workspaceMemberStateQuery:
		return fakeRow{values: []any{queries.memberRole, queries.memberStatus, queries.mutationKey, queries.mutationHash}}
	case workspaceMemberLegacyStateQuery:
		return fakeRow{values: []any{queries.memberRole, queries.memberStatus, queries.mutationKey}}
	case workspaceMemberAdminCountQuery:
		return fakeRow{values: []any{int64(2)}}
	case workspaceMemberUpdateQuery:
		return fakeRow{values: queries.memberValues}
	case workspaceMemberLegacyUpdateQuery:
		return fakeRow{values: queries.memberValues}
	default:
		return fakeRow{err: errors.New("unexpected member query")}
	}
}

func (queries *memberMutationQueries) QueryContext(context.Context, string, ...any) (rowsScanner, error) {
	return emptyRows{}, nil
}

func equalStrings(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

func newFakeQueries() *fakeQueries {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	return &fakeQueries{
		organizationVals: []any{organizationA, "Synthetic Organization", "active", int64(1), now, now},
		workspaceValues:  []any{workspaceA, organizationA, "Synthetic Workspace", "active", int64(1), now, now},
		storeValues:      []any{storeA, organizationA, workspaceA, "synthetic-store", "Synthetic Store", "store", "active", int64(1), now, now},
	}
}

func (queries *fakeQueries) QueryRowContext(_ context.Context, statement string, arguments ...any) rowScanner {
	if statement == applyScopeStatement {
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
	queries.queryCount++
	if len(arguments) < 2 || arguments[0] != queries.scopeOrganization || arguments[1] != queries.scopeWorkspace {
		return fakeRow{err: sql.ErrNoRows}
	}
	if queries.scopeOrganization != organizationA || queries.scopeWorkspace != workspaceA {
		return fakeRow{err: sql.ErrNoRows}
	}
	switch statement {
	case organizationQuery:
		return fakeRow{values: queries.organizationVals}
	case workspaceQuery:
		return fakeRow{values: queries.workspaceValues}
	case storeQuery:
		if len(arguments) != 3 || arguments[2] != storeA {
			return fakeRow{err: sql.ErrNoRows}
		}
		return fakeRow{values: queries.storeValues}
	default:
		return fakeRow{err: errors.New("unexpected query")}
	}
}

func (queries *fakeQueries) QueryContext(context.Context, string, ...any) (rowsScanner, error) {
	return emptyRows{}, nil
}

type emptyRows struct{}

func (emptyRows) Next() bool        { return false }
func (emptyRows) Scan(...any) error { return sql.ErrNoRows }
func (emptyRows) Err() error        { return nil }
func (emptyRows) Close() error      { return nil }

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
		switch destination := destinations[index].(type) {
		case *string:
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("value %d is not string", index)
			}
			*destination = text
		case *int64:
			number, ok := value.(int64)
			if !ok {
				return fmt.Errorf("value %d is not int64", index)
			}
			*destination = number
		case *int:
			number, ok := value.(int64)
			if !ok {
				return fmt.Errorf("value %d is not int64", index)
			}
			*destination = int(number)
		case *bool:
			value, ok := value.(bool)
			if !ok {
				return fmt.Errorf("value %d is not bool", index)
			}
			*destination = value
		case *time.Time:
			timestamp, ok := value.(time.Time)
			if !ok {
				return fmt.Errorf("value %d is not time.Time", index)
			}
			*destination = timestamp
		default:
			return fmt.Errorf("unsupported scan destination %T", destination)
		}
	}
	return nil
}

func mustScope(t *testing.T, organizationID, workspaceID string) tenancy.Scope {
	t.Helper()
	scope, err := tenancy.ParseScope(organizationID, workspaceID)
	if err != nil {
		t.Fatalf("ParseScope() error = %v", err)
	}
	return scope
}
