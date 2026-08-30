// Package returnsrepo persists the tenant-scoped cancellation/return
// aggregates and their append-only operational evidence.
package returnsrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	core "github.com/torgnexa/torgnexa/internal/core/returns"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

type Repository struct{ db *sql.DB }

var _ core.Repository = (*Repository)(nil)

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("returns repository: database is required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Cancellation(ctx context.Context, scope core.Scope, id core.CancellationID) (core.CancellationRequest, error) {
	if err := r.validate(ctx, scope); err != nil || !id.Valid() {
		return core.CancellationRequest{}, core.ErrInvalidRecord
	}
	var result core.CancellationRequest
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		result, err = scanCancellation(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,order_id,status,reason_code,source,idempotency_key,version,created_at,updated_at FROM order_cancellations WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id.String()))
		return err
	})
	return result, err
}

func (r *Repository) CreateCancellation(ctx context.Context, scope core.Scope, command core.CancellationRequest, mutation core.Mutation) (core.CancellationRequest, error) {
	if err := r.validateMutation(ctx, scope, command.OrganizationID, command.WorkspaceID, mutation); err != nil || command.Validate() != nil {
		return core.CancellationRequest{}, core.ErrInvalidRecord
	}
	var result core.CancellationRequest
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM orders WHERE organization_id=$1 AND workspace_id=$2 AND id=$3)`, scope.OrganizationID(), scope.WorkspaceID(), command.OrderID).Scan(&exists); err != nil {
			return fmt.Errorf("returns repository: order lookup: %w", err)
		}
		if !exists {
			return core.ErrNotFound
		}
		var inserted string
		err := tx.QueryRowContext(ctx, `INSERT INTO order_cancellations(id,organization_id,workspace_id,order_id,status,reason_code,source,idempotency_key,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$9) ON CONFLICT (organization_id,workspace_id,idempotency_key) DO NOTHING RETURNING id`, command.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), command.OrderID, command.Status, command.ReasonCode, command.Source, command.IdempotencyKey, mutation.OccurredAt).Scan(&inserted)
		if errors.Is(err, sql.ErrNoRows) {
			result, err = scanCancellation(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,order_id,status,reason_code,source,idempotency_key,version,created_at,updated_at FROM order_cancellations WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, scope.OrganizationID(), scope.WorkspaceID(), command.IdempotencyKey))
			if err != nil || result.OrderID != command.OrderID || result.ReasonCode != command.ReasonCode {
				return core.ErrConflict
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("returns repository: insert cancellation: %w", err)
		}
		if inserted != command.ID.String() {
			return core.ErrConflict
		}
		result, err = scanCancellation(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,order_id,status,reason_code,source,idempotency_key,version,created_at,updated_at FROM order_cancellations WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String()))
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "returns.cancellation.created", "order_cancellation", result.ID.String(), audit.RiskWriteSensitive, audit.Summary{"order_id": result.OrderID, "status": string(result.Status), "reason_code": result.ReasonCode}); err != nil {
			return err
		}
		return enqueue(ctx, tx, scope, mutation, "commerce.orders.cancellation_requested.v1", "order_cancellation", result.ID.String(), map[string]any{"cancellation_id": result.ID.String(), "order_id": result.OrderID, "status": result.Status, "reason_code": result.ReasonCode, "version": result.Version})
	})
	return result, err
}

func (r *Repository) ChangeCancellationStatus(ctx context.Context, scope core.Scope, id core.CancellationID, status core.CancellationStatus, expected int64, mutation core.Mutation) (core.CancellationRequest, error) {
	if err := r.validateMutation(ctx, scope, "", "", mutation); err != nil || !id.Valid() || !status.Valid() || expected < 1 {
		return core.CancellationRequest{}, core.ErrInvalidRecord
	}
	var result core.CancellationRequest
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		current, err := scanCancellation(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,order_id,status,reason_code,source,idempotency_key,version,created_at,updated_at FROM order_cancellations WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), id.String()))
		if err != nil {
			return err
		}
		if current.Version != expected || core.ValidateCancellationTransition(current.Status, status) != nil {
			return core.ErrConflict
		}
		result, err = scanCancellation(tx.QueryRowContext(ctx, `UPDATE order_cancellations SET status=$4,version=version+1,updated_at=$5 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$6 RETURNING id,organization_id,workspace_id,order_id,status,reason_code,source,idempotency_key,version,created_at,updated_at`, scope.OrganizationID(), scope.WorkspaceID(), id.String(), status, mutation.OccurredAt, expected))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO cancellation_state_history(id,organization_id,workspace_id,cancellation_id,from_status,to_status,reason_code,actor_id,correlation_id,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, mutation.AuditID, scope.OrganizationID(), scope.WorkspaceID(), id.String(), current.Status, status, current.ReasonCode, mutation.ActorID, mutation.CorrelationID, mutation.OccurredAt); err != nil {
			return fmt.Errorf("returns repository: cancellation history: %w", err)
		}
		if err := appendAudit(ctx, tx, scope, mutation, "returns.cancellation.status_changed", "order_cancellation", id.String(), audit.RiskWriteSensitive, audit.Summary{"from": string(current.Status), "to": string(status), "order_id": result.OrderID}); err != nil {
			return err
		}
		return enqueue(ctx, tx, scope, mutation, "commerce.orders.cancellation_state_changed.v1", "order_cancellation", id.String(), map[string]any{"cancellation_id": id.String(), "order_id": result.OrderID, "from_status": current.Status, "status": result.Status, "version": result.Version})
	})
	return result, err
}

func (r *Repository) Return(ctx context.Context, scope core.Scope, id core.ReturnID) (core.ReturnRequest, error) {
	if err := r.validate(ctx, scope); err != nil || !id.Valid() {
		return core.ReturnRequest{}, core.ErrInvalidRecord
	}
	var result core.ReturnRequest
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		result, err = scanReturn(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,order_id,status,reason_code,source,currency,requested_shipping_minor,requested_tax_minor,idempotency_key,version,created_at,updated_at FROM commerce_returns WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), id.String()))
		return err
	})
	return result, err
}

func (r *Repository) CreateReturn(ctx context.Context, scope core.Scope, command core.ReturnRequest, mutation core.Mutation) (core.ReturnRequest, error) {
	if err := r.validateMutation(ctx, scope, command.OrganizationID, command.WorkspaceID, mutation); err != nil || command.Validate() != nil {
		return core.ReturnRequest{}, core.ErrInvalidRecord
	}
	var result core.ReturnRequest
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM orders WHERE organization_id=$1 AND workspace_id=$2 AND id=$3)`, scope.OrganizationID(), scope.WorkspaceID(), command.OrderID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return core.ErrNotFound
		}
		var inserted string
		err := tx.QueryRowContext(ctx, `INSERT INTO commerce_returns(id,organization_id,workspace_id,order_id,status,reason_code,source,currency,requested_shipping_minor,requested_tax_minor,idempotency_key,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,1,$12,$12) ON CONFLICT (organization_id,workspace_id,idempotency_key) DO NOTHING RETURNING id`, command.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), command.OrderID, command.Status, command.ReasonCode, command.Source, command.Currency.String(), command.RequestedShippingMinor, command.RequestedTaxMinor, command.IdempotencyKey, mutation.OccurredAt).Scan(&inserted)
		if errors.Is(err, sql.ErrNoRows) {
			result, err = scanReturn(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,order_id,status,reason_code,source,currency,requested_shipping_minor,requested_tax_minor,idempotency_key,version,created_at,updated_at FROM commerce_returns WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, scope.OrganizationID(), scope.WorkspaceID(), command.IdempotencyKey))
			if err != nil || result.OrderID != command.OrderID || result.Currency != command.Currency {
				return core.ErrConflict
			}
			return nil
		}
		if err != nil {
			return err
		}
		if inserted != command.ID.String() {
			return core.ErrConflict
		}
		result, err = scanReturn(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,order_id,status,reason_code,source,currency,requested_shipping_minor,requested_tax_minor,idempotency_key,version,created_at,updated_at FROM commerce_returns WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String()))
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "returns.return.created", "return", result.ID.String(), audit.RiskWriteSensitive, audit.Summary{"order_id": result.OrderID, "status": string(result.Status), "reason_code": result.ReasonCode}); err != nil {
			return err
		}
		return enqueue(ctx, tx, scope, mutation, "commerce.returns.requested.v1", "return", result.ID.String(), map[string]any{"return_id": result.ID.String(), "order_id": result.OrderID, "status": result.Status, "currency": result.Currency.String(), "version": result.Version})
	})
	return result, err
}

func (r *Repository) ChangeReturnStatus(ctx context.Context, scope core.Scope, id core.ReturnID, status core.ReturnStatus, expected int64, mutation core.Mutation) (core.ReturnRequest, error) {
	if err := r.validateMutation(ctx, scope, "", "", mutation); err != nil || !id.Valid() || !status.Valid() || expected < 1 {
		return core.ReturnRequest{}, core.ErrInvalidRecord
	}
	var result core.ReturnRequest
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		current, err := scanReturn(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,order_id,status,reason_code,source,currency,requested_shipping_minor,requested_tax_minor,idempotency_key,version,created_at,updated_at FROM commerce_returns WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), id.String()))
		if err != nil {
			return err
		}
		if current.Version != expected || core.ValidateReturnTransition(current.Status, status) != nil {
			return core.ErrConflict
		}
		result, err = scanReturn(tx.QueryRowContext(ctx, `UPDATE commerce_returns SET status=$4,version=version+1,updated_at=$5 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$6 RETURNING id,organization_id,workspace_id,order_id,status,reason_code,source,currency,requested_shipping_minor,requested_tax_minor,idempotency_key,version,created_at,updated_at`, scope.OrganizationID(), scope.WorkspaceID(), id.String(), status, mutation.OccurredAt, expected))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO return_state_history(id,organization_id,workspace_id,return_id,from_status,to_status,reason_code,actor_id,correlation_id,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, mutation.AuditID, scope.OrganizationID(), scope.WorkspaceID(), id.String(), current.Status, status, current.ReasonCode, mutation.ActorID, mutation.CorrelationID, mutation.OccurredAt); err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "returns.return.status_changed", "return", id.String(), audit.RiskWriteSensitive, audit.Summary{"from": string(current.Status), "to": string(status), "order_id": result.OrderID}); err != nil {
			return err
		}
		return enqueue(ctx, tx, scope, mutation, "commerce.returns.state_changed.v1", "return", id.String(), map[string]any{"return_id": id.String(), "order_id": result.OrderID, "from_status": current.Status, "status": result.Status, "version": result.Version})
	})
	return result, err
}

func (r *Repository) ListReturns(ctx context.Context, scope core.Scope, limit int) ([]core.ReturnRequest, error) {
	if err := r.validate(ctx, scope); err != nil || limit < 1 || limit > 200 {
		return nil, core.ErrInvalidRecord
	}
	items := make([]core.ReturnRequest, 0, limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,organization_id,workspace_id,order_id,status,reason_code,source,currency,requested_shipping_minor,requested_tax_minor,idempotency_key,version,created_at,updated_at FROM commerce_returns WHERE organization_id=$1 AND workspace_id=$2 ORDER BY created_at DESC,id DESC LIMIT $3`, scope.OrganizationID(), scope.WorkspaceID(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, scanErr := scanReturn(rows)
			if scanErr != nil {
				return scanErr
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

func (r *Repository) ReturnItems(ctx context.Context, scope core.Scope, id core.ReturnID, limit int) ([]core.ReturnItem, error) {
	if err := r.validate(ctx, scope); err != nil || !id.Valid() || limit < 1 || limit > 200 {
		return nil, core.ErrInvalidRecord
	}
	items := make([]core.ReturnItem, 0, limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,return_id,order_item_id,requested_coefficient,requested_scale,received_coefficient,received_scale,accepted_coefficient,accepted_scale,unit,disposition,version,created_at,updated_at FROM return_items WHERE organization_id=$1 AND workspace_id=$2 AND return_id=$3 ORDER BY id LIMIT $4`, scope.OrganizationID(), scope.WorkspaceID(), id.String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, scanErr := scanReturnItem(rows)
			if scanErr != nil {
				return scanErr
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

func (r *Repository) CreateReturnItem(ctx context.Context, scope core.Scope, item core.ReturnItem, mutation core.Mutation) (core.ReturnItem, error) {
	if err := r.validateMutation(ctx, scope, "", "", mutation); err != nil || item.Validate() != nil {
		return core.ReturnItem{}, core.ErrInvalidRecord
	}
	var result core.ReturnItem
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO return_items(id,organization_id,workspace_id,return_id,order_item_id,requested_coefficient,requested_scale,received_coefficient,received_scale,accepted_coefficient,accepted_scale,unit,disposition,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,1,$14,$14) ON CONFLICT DO NOTHING`, item.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), item.ReturnID.String(), item.OrderItemID, item.Requested.Coefficient, item.Requested.Scale, item.Received.Coefficient, item.Received.Scale, item.Accepted.Coefficient, item.Accepted.Scale, item.Requested.Unit, item.Disposition, mutation.OccurredAt)
		if err != nil {
			return err
		}
		result, err = scanReturnItem(tx.QueryRowContext(ctx, `SELECT id,return_id,order_item_id,requested_coefficient,requested_scale,received_coefficient,received_scale,accepted_coefficient,accepted_scale,unit,disposition,version,created_at,updated_at FROM return_items WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), item.ID.String()))
		if err != nil {
			return err
		}
		return appendAudit(ctx, tx, scope, mutation, "returns.return_item.created", "return_item", item.ID.String(), audit.RiskWriteSensitive, audit.Summary{"return_id": item.ReturnID.String(), "order_item_id": item.OrderItemID, "disposition": string(item.Disposition)})
	})
	return result, err
}

func (r *Repository) RecordInspection(ctx context.Context, scope core.Scope, inspection core.InspectionResult, mutation core.Mutation) error {
	if err := r.validateMutation(ctx, scope, "", "", mutation); err != nil || inspection.Validate() != nil {
		return core.ErrInvalidRecord
	}
	return r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO return_inspections(id,organization_id,workspace_id,return_id,return_item_id,outcome,condition_code,discrepancy_code,quantity_coefficient,quantity_scale,unit,disposition,artifact_ref,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10,$11,$12,NULLIF($13,''),$14) ON CONFLICT DO NOTHING`, inspection.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), inspection.ReturnID.String(), inspection.ReturnItemID.String(), inspection.Outcome, inspection.ConditionCode, inspection.DiscrepancyCode, inspection.Quantity.Coefficient, inspection.Quantity.Scale, inspection.Quantity.Unit, inspection.Disposition, inspection.ArtifactRef, inspection.OccurredAt)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			return nil
		}
		return appendAudit(ctx, tx, scope, mutation, "returns.inspection.recorded", "return_inspection", inspection.ID.String(), audit.RiskWriteSensitive, audit.Summary{"return_id": inspection.ReturnID.String(), "return_item_id": inspection.ReturnItemID.String(), "outcome": string(inspection.Outcome), "disposition": string(inspection.Disposition)})
	})
}

func (r *Repository) CreateRefundAllocation(ctx context.Context, scope core.Scope, allocation core.RefundAllocation, mutation core.Mutation) (core.RefundAllocation, error) {
	if err := r.validateMutation(ctx, scope, "", "", mutation); err != nil || allocation.Validate() != nil {
		return core.RefundAllocation{}, core.ErrInvalidRecord
	}
	var result core.RefundAllocation
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var paymentAmount int64
		var paymentCurrency string
		if err := tx.QueryRowContext(ctx, `SELECT amount_minor_units,currency FROM payments WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 FOR SHARE`, scope.OrganizationID(), scope.WorkspaceID(), allocation.PaymentID).Scan(&paymentAmount, &paymentCurrency); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return core.ErrNotFound
			}
			return err
		}
		if paymentCurrency != allocation.Currency.String() {
			return core.ErrInvalidRecord
		}
		var allocated int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_minor_units),0) FROM refund_allocations WHERE organization_id=$1 AND workspace_id=$2 AND payment_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), allocation.PaymentID).Scan(&allocated); err != nil {
			return err
		}
		if allocation.Amount.MinorUnits() > paymentAmount || allocated > paymentAmount-allocation.Amount.MinorUnits() {
			return core.ErrOverAllocated
		}
		var err error
		result, err = scanRefundAllocation(tx.QueryRowContext(ctx, `INSERT INTO refund_allocations(id,organization_id,workspace_id,payment_id,refund_id,return_id,order_item_id,component,amount_minor_units,currency,idempotency_key,version,created_at) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10,$11,1,$12) ON CONFLICT (organization_id,workspace_id,idempotency_key) DO UPDATE SET id=refund_allocations.id RETURNING id,organization_id,workspace_id,payment_id,refund_id,return_id,COALESCE(order_item_id,''),component,amount_minor_units,currency,idempotency_key,version,created_at`, allocation.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), allocation.PaymentID, allocation.RefundID, allocation.ReturnID.String(), allocation.OrderItemID, allocation.Component, allocation.Amount.MinorUnits(), allocation.Currency.String(), allocation.IdempotencyKey, mutation.OccurredAt))
		if err != nil {
			return err
		}
		if result.ID != allocation.ID {
			if result.PaymentID != allocation.PaymentID || result.ReturnID != allocation.ReturnID || result.Amount != allocation.Amount || result.Component != allocation.Component {
				return core.ErrConflict
			}
			return nil
		}
		return appendAudit(ctx, tx, scope, mutation, "returns.refund_allocation.created", "refund_allocation", allocation.ID.String(), audit.RiskLegallySignificant, audit.Summary{"payment_id": allocation.PaymentID, "return_id": allocation.ReturnID.String(), "component": string(allocation.Component), "amount_minor_units": allocation.Amount.MinorUnits(), "currency": allocation.Currency.String()})
	})
	return result, err
}

func (r *Repository) ListEvidence(ctx context.Context, scope core.Scope, operationID string, limit int) ([]core.OperationEvidence, error) {
	if err := r.validate(ctx, scope); err != nil || (operationID != "" && len(operationID) > 192) || limit < 1 || limit > 200 {
		return nil, core.ErrInvalidRecord
	}
	items := make([]core.OperationEvidence, 0, limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		query := `SELECT id,organization_id,workspace_id,operation_type,operation_id,outcome,COALESCE(reason_code,''),COALESCE(remote_id,''),digest,correlation_id,COALESCE(causation_id,''),occurred_at FROM commerce_operation_evidence WHERE organization_id=$1 AND workspace_id=$2`
		args := []any{scope.OrganizationID(), scope.WorkspaceID()}
		if operationID != "" {
			query += ` AND operation_id=$3`
			args = append(args, operationID)
		}
		query += fmt.Sprintf(` ORDER BY occurred_at DESC,id DESC LIMIT $%d`, len(args)+1)
		args = append(args, limit)
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, scanErr := scanEvidence(rows)
			if scanErr != nil {
				return scanErr
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

func (r *Repository) validate(ctx context.Context, scope core.Scope) error {
	if ctx == nil || ctx.Err() != nil || r == nil || r.db == nil || !scope.Valid() {
		return core.ErrInvalidScope
	}
	return nil
}
func (r *Repository) validateMutation(ctx context.Context, scope core.Scope, org, workspace string, mutation core.Mutation) error {
	if err := r.validate(ctx, scope); err != nil {
		return err
	}
	if (org != "" && org != scope.OrganizationID()) || (workspace != "" && workspace != scope.WorkspaceID()) {
		return core.ErrInvalidScope
	}
	return mutation.Validate()
}

func (r *Repository) tx(ctx context.Context, scope core.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("returns repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var org, workspace string
	if err := tx.QueryRowContext(ctx, applyScope, scope.OrganizationID(), scope.WorkspaceID()).Scan(&org, &workspace); err != nil {
		return fmt.Errorf("returns repository: scope: %w", err)
	}
	if org != scope.OrganizationID() || workspace != scope.WorkspaceID() {
		return core.ErrInvalidScope
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("returns repository: commit: %w", err)
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanCancellation(row scanner) (core.CancellationRequest, error) {
	var result core.CancellationRequest
	var id, org, ws, status string
	if err := row.Scan(&id, &org, &ws, &result.OrderID, &status, &result.ReasonCode, &result.Source, &result.IdempotencyKey, &result.Version, &result.CreatedAt, &result.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.CancellationRequest{}, core.ErrNotFound
		}
		return core.CancellationRequest{}, err
	}
	result.ID, result.OrganizationID, result.WorkspaceID, result.Status = core.CancellationID(id), org, ws, core.CancellationStatus(status)
	result.CreatedAt, result.UpdatedAt = result.CreatedAt.UTC(), result.UpdatedAt.UTC()
	if err := result.Validate(); err != nil {
		return core.CancellationRequest{}, err
	}
	return result, nil
}

func scanReturn(row scanner) (core.ReturnRequest, error) {
	var result core.ReturnRequest
	var id, org, ws, status, currency string
	if err := row.Scan(&id, &org, &ws, &result.OrderID, &status, &result.ReasonCode, &result.Source, &currency, &result.RequestedShippingMinor, &result.RequestedTaxMinor, &result.IdempotencyKey, &result.Version, &result.CreatedAt, &result.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.ReturnRequest{}, core.ErrNotFound
		}
		return core.ReturnRequest{}, err
	}
	result.ID, result.OrganizationID, result.WorkspaceID, result.Status = core.ReturnID(id), org, ws, core.ReturnStatus(status)
	result.Currency = domain.Currency(currency)
	result.CreatedAt, result.UpdatedAt = result.CreatedAt.UTC(), result.UpdatedAt.UTC()
	if err := result.Validate(); err != nil {
		return core.ReturnRequest{}, err
	}
	return result, nil
}

func scanReturnItem(row scanner) (core.ReturnItem, error) {
	var result core.ReturnItem
	var id, returnID, disposition, unit string
	var requestedCoefficient, receivedCoefficient, acceptedCoefficient int64
	var requestedScale, receivedScale, acceptedScale int
	if err := row.Scan(&id, &returnID, &result.OrderItemID, &requestedCoefficient, &requestedScale, &receivedCoefficient, &receivedScale, &acceptedCoefficient, &acceptedScale, &unit, &disposition, &result.Version, &result.CreatedAt, &result.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.ReturnItem{}, core.ErrNotFound
		}
		return core.ReturnItem{}, err
	}
	var err error
	result.ID, err = core.ParseReturnItemID(id)
	if err != nil {
		return core.ReturnItem{}, err
	}
	result.ReturnID, err = core.ParseReturnID(returnID)
	if err != nil {
		return core.ReturnItem{}, err
	}
	if result.Requested, err = core.NewQuantity(requestedCoefficient, uint8(requestedScale), unit); err != nil {
		return core.ReturnItem{}, err
	}
	if result.Received, err = core.NewQuantity(receivedCoefficient, uint8(receivedScale), unit); err != nil {
		return core.ReturnItem{}, err
	}
	if result.Accepted, err = core.NewQuantity(acceptedCoefficient, uint8(acceptedScale), unit); err != nil {
		return core.ReturnItem{}, err
	}
	result.Disposition, result.CreatedAt, result.UpdatedAt = core.Disposition(disposition), result.CreatedAt.UTC(), result.UpdatedAt.UTC()
	if err := result.Validate(); err != nil {
		return core.ReturnItem{}, err
	}
	return result, nil
}

func scanRefundAllocation(row scanner) (core.RefundAllocation, error) {
	var result core.RefundAllocation
	var id, org, ws, paymentID, returnID, orderItemID, component, currency string
	var minor int64
	var refundID string
	if err := row.Scan(&id, &org, &ws, &paymentID, &refundID, &returnID, &orderItemID, &component, &minor, &currency, &result.IdempotencyKey, &result.Version, &result.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.RefundAllocation{}, core.ErrNotFound
		}
		return core.RefundAllocation{}, err
	}
	var err error
	result.ID, err = core.ParseRefundAllocationID(id)
	if err != nil {
		return core.RefundAllocation{}, err
	}
	result.ReturnID, err = core.ParseReturnID(returnID)
	if err != nil {
		return core.RefundAllocation{}, err
	}
	result.OrganizationID, result.WorkspaceID, result.PaymentID, result.RefundID, result.OrderItemID, result.Component, result.Currency = org, ws, paymentID, refundID, orderItemID, core.RefundComponent(component), domain.Currency(currency)
	result.Amount, err = domain.NewMoney(minor, result.Currency)
	if err != nil {
		return core.RefundAllocation{}, err
	}
	if err := result.Validate(); err != nil {
		return core.RefundAllocation{}, err
	}
	return result, nil
}

func scanEvidence(row scanner) (core.OperationEvidence, error) {
	var result core.OperationEvidence
	var id, org, ws string
	if err := row.Scan(&id, &org, &ws, &result.OperationType, &result.OperationID, &result.Outcome, &result.ReasonCode, &result.RemoteID, &result.Digest, &result.CorrelationID, &result.CausationID, &result.OccurredAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.OperationEvidence{}, core.ErrNotFound
		}
		return core.OperationEvidence{}, err
	}
	result.ID, _ = core.ParseEvidenceID(id)
	result.OrganizationID, result.WorkspaceID = org, ws
	result.OccurredAt = result.OccurredAt.UTC()
	if err := result.Validate(); err != nil {
		return core.OperationEvidence{}, err
	}
	return result, nil
}

func appendAudit(ctx context.Context, tx *sql.Tx, scope core.Scope, mutation core.Mutation, action, resourceType, resourceID string, risk audit.Risk, summary audit.Summary) error {
	ts, err := tenancy.ParseScope(scope.OrganizationID(), scope.WorkspaceID())
	if err != nil {
		return err
	}
	safe, err := audit.SanitizeSummary(summary)
	if err != nil {
		return err
	}
	record := audit.Record{ID: mutation.AuditID, OrganizationID: ts.OrganizationID(), WorkspaceID: ts.WorkspaceID(), ActorID: mutation.ActorID, Source: mutation.Source, Action: action, ResourceType: resourceType, ResourceID: resourceID, CorrelationID: mutation.CorrelationID, Risk: risk, Summary: safe, CreatedAt: mutation.OccurredAt}
	return auditrepo.AppendTransaction(ctx, tx, ts, record)
}

func enqueue(ctx context.Context, tx *sql.Tx, scope core.Scope, mutation core.Mutation, eventType, entityType, entityID string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	typ, err := eventbus.ParseEventType(eventType)
	if err != nil {
		return err
	}
	at, err := domain.NewUTCInstant(mutation.OccurredAt)
	if err != nil {
		return err
	}
	event := eventbus.Event{ID: mutation.EventID, Type: typ, OccurredAt: at, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), EntityType: entityType, EntityID: entityID, Source: mutation.Source, CorrelationID: mutation.CorrelationID, CausationID: mutation.CausationID, ActorID: mutation.ActorID, Data: data}
	if err := event.Validate(); err != nil {
		return err
	}
	enqueuer, err := outboxrepo.NewTransactionEnqueuer(tx)
	if err != nil {
		return err
	}
	return enqueuer.Enqueue(ctx, event)
}
