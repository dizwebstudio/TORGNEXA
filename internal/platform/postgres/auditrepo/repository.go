// Package auditrepo implements the tenant-scoped PostgreSQL audit repository.
package auditrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
)

const applyScopeStatement = `SELECT
  set_config('app.organization_id', $1, true),
  set_config('app.workspace_id', $2, true)`

const appendStatement = `INSERT INTO audit_records (
  id, organization_id, workspace_id, actor_id, source, action,
  resource_type, resource_id, correlation_id, risk, summary, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12)`

// Repository appends audit rows using a transaction-local tenant RLS scope.
type Repository struct {
	transactions transactor
	database     *sql.DB
}

var _ audit.Repository = (*Repository)(nil)

// New constructs an audit repository over an application database pool. The
// database role must be subject to forced RLS and must not hold BYPASSRLS.
func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("audit repository: database is required")
	}
	return &Repository{transactions: sqlTransactor{database: database}, database: database}, nil
}

// List returns a newest-first tenant-scoped page. Audit identifiers are UUIDv7,
// so the opaque cursor preserves stable chronological pagination.
func (repository *Repository) List(ctx context.Context, scope tenancy.Scope, limit int, cursor string) ([]audit.Record, string, error) {
	return repository.list(ctx, scope, limit, cursor, false)
}

// ListSettings returns only immutable evidence created by settings surfaces.
func (repository *Repository) ListSettings(ctx context.Context, scope tenancy.Scope, limit int, cursor string) ([]audit.Record, string, error) {
	return repository.list(ctx, scope, limit, cursor, true)
}

func (repository *Repository) list(ctx context.Context, scope tenancy.Scope, limit int, cursor string, settingsOnly bool) ([]audit.Record, string, error) {
	if ctx == nil || !scope.Valid() || repository == nil || repository.database == nil || limit < 1 || limit > 200 {
		return nil, "", audit.ErrInvalidRecord
	}
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, "", fmt.Errorf("begin audit list: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var organizationID, workspaceID string
	if err := tx.QueryRowContext(ctx, applyScopeStatement, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&organizationID, &workspaceID); err != nil {
		return nil, "", fmt.Errorf("apply tenant scope: %w", err)
	}
	statement := `SELECT id,organization_id,workspace_id,actor_id,source,action,resource_type,resource_id,correlation_id,risk,summary,created_at FROM audit_records WHERE organization_id=$1 AND workspace_id=$2`
	arguments := []any{scope.OrganizationID().String(), scope.WorkspaceID().String()}
	if settingsOnly {
		statement += ` AND (action LIKE 'settings.%' OR action LIKE 'connector.account.%')`
	}
	if cursor != "" {
		statement += ` AND id < $3`
		arguments = append(arguments, cursor)
	}
	statement += fmt.Sprintf(` ORDER BY id DESC LIMIT $%d`, len(arguments)+1)
	arguments = append(arguments, limit+1)
	rows, err := tx.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, "", fmt.Errorf("query audit records: %w", err)
	}
	defer rows.Close()
	records := make([]audit.Record, 0, limit)
	for rows.Next() {
		var record audit.Record
		var organizationRaw, workspaceRaw, risk string
		var summary []byte
		var createdAt time.Time
		if err := rows.Scan(&record.ID, &organizationRaw, &workspaceRaw, &record.ActorID, &record.Source, &record.Action, &record.ResourceType, &record.ResourceID, &record.CorrelationID, &risk, &summary, &createdAt); err != nil {
			return nil, "", fmt.Errorf("scan audit record: %w", err)
		}
		organization, err := tenancy.ParseOrganizationID(organizationRaw)
		if err != nil {
			return nil, "", audit.ErrInvalidRecord
		}
		workspace, err := tenancy.ParseWorkspaceID(workspaceRaw)
		if err != nil || json.Unmarshal(summary, &record.Summary) != nil {
			return nil, "", audit.ErrInvalidRecord
		}
		record.OrganizationID, record.WorkspaceID, record.Risk, record.CreatedAt = organization, workspace, audit.Risk(risk), createdAt.UTC()
		if record.OrganizationID != scope.OrganizationID() || record.WorkspaceID != scope.WorkspaceID() || record.ID == "" || record.ActorID == "" || record.Action == "" || record.ResourceID == "" || record.CreatedAt.IsZero() {
			return nil, "", audit.ErrInvalidRecord
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate audit records: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("commit audit list: %w", err)
	}
	next := ""
	if len(records) > limit {
		next = records[limit-1].ID
		records = records[:limit]
	}
	return records, next, nil
}

func newRepository(transactions transactor) *Repository {
	return &Repository{transactions: transactions}
}

// Append persists exactly one immutable tenant-scoped record.
func (repository *Repository) Append(ctx context.Context, scope tenancy.Scope, record audit.Record) error {
	if ctx == nil {
		return errors.New("audit repository: context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("audit repository: %w", err)
	}
	if !scope.Valid() {
		return tenancy.ErrInvalidScope
	}
	if repository == nil || repository.transactions == nil {
		return errors.New("audit repository: repository is not initialized")
	}
	if record.OrganizationID != scope.OrganizationID() || record.WorkspaceID != scope.WorkspaceID() {
		return audit.ErrInvalidRecord
	}
	if err := audit.ValidateRecord(record); err != nil {
		return err
	}
	summary, err := json.Marshal(record.Summary)
	if err != nil {
		return audit.ErrInvalidRecord
	}

	return repository.transactions.readWrite(ctx, func(queries queryer) error {
		var appliedOrganization, appliedWorkspace string
		if err := queries.QueryRowContext(
			ctx,
			applyScopeStatement,
			scope.OrganizationID().String(),
			scope.WorkspaceID().String(),
		).Scan(&appliedOrganization, &appliedWorkspace); err != nil {
			return fmt.Errorf("apply tenant scope: %w", err)
		}
		if appliedOrganization != scope.OrganizationID().String() || appliedWorkspace != scope.WorkspaceID().String() {
			return fmt.Errorf("apply tenant scope: %w", tenancy.ErrInvalidScope)
		}

		result, err := queries.ExecContext(
			ctx,
			appendStatement,
			record.ID,
			record.OrganizationID.String(),
			record.WorkspaceID.String(),
			record.ActorID,
			record.Source,
			record.Action,
			record.ResourceType,
			record.ResourceID,
			record.CorrelationID,
			string(record.Risk),
			string(summary),
			record.CreatedAt.UTC(),
		)
		if err != nil {
			return fmt.Errorf("append audit record: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("append audit record result: %w", err)
		}
		if rows != 1 {
			return fmt.Errorf("append audit record: affected %d rows", rows)
		}
		return nil
	})
}

type rowScanner interface {
	Scan(...any) error
}

type result interface {
	RowsAffected() (int64, error)
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) rowScanner
	ExecContext(context.Context, string, ...any) (result, error)
}

type transactor interface {
	readWrite(context.Context, func(queryer) error) error
}

type sqlTransactor struct {
	database *sql.DB
}

func (transactions sqlTransactor) readWrite(ctx context.Context, operation func(queryer) error) error {
	transaction, err := transactions.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin audit transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback()
	}()
	queries := sqlQueries{transaction: transaction}
	if err := operation(queries); err != nil {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return errors.Join(err, fmt.Errorf("rollback audit transaction: %w", rollbackErr))
		}
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit audit transaction: %w", err)
	}
	return nil
}

type sqlQueries struct {
	transaction *sql.Tx
}

func (queries sqlQueries) QueryRowContext(ctx context.Context, statement string, arguments ...any) rowScanner {
	return queries.transaction.QueryRowContext(ctx, statement, arguments...)
}

func (queries sqlQueries) ExecContext(ctx context.Context, statement string, arguments ...any) (result, error) {
	return queries.transaction.ExecContext(ctx, statement, arguments...)
}
