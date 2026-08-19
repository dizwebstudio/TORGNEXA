package auditrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
)

// AppendTransaction appends a validated immutable audit record inside a caller-owned
// SQL transaction. The caller owns commit/rollback, allowing domain state, audit,
// and Transactional Outbox intent to share one atomic boundary.
func AppendTransaction(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, record audit.Record) error {
	if ctx == nil || tx == nil {
		return errors.New("audit repository: context and transaction are required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !scope.Valid() || record.OrganizationID != scope.OrganizationID() || record.WorkspaceID != scope.WorkspaceID() {
		return audit.ErrInvalidRecord
	}
	if err := audit.ValidateRecord(record); err != nil {
		return err
	}
	summary, err := json.Marshal(record.Summary)
	if err != nil {
		return audit.ErrInvalidRecord
	}
	result, err := tx.ExecContext(ctx, appendStatement,
		record.ID, record.OrganizationID.String(), record.WorkspaceID.String(), record.ActorID,
		record.Source, record.Action, record.ResourceType, record.ResourceID, record.CorrelationID,
		string(record.Risk), string(summary), record.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("append transactional audit record: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("append transactional audit result: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("append transactional audit record: affected %d rows", rows)
	}
	return nil
}
