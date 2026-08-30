package inventoryrepo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/inventory"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
)

var (
	ErrWMSTaskState      = errors.New("inventory repository: invalid wms task state")
	ErrWMSTaskAssignment = errors.New("inventory repository: wms task is assigned to another operator")
	ErrWMSBatchState     = errors.New("inventory repository: invalid wms batch state")
)

// WMSTask is the public repository view of a durable provider-neutral WMS task.
// Quantities stay exact and are never represented by float64.
type WMSTask struct {
	ID, IdempotencyKey, TaskType, State     string
	WarehouseID, SKU                        string
	OrderID, OrderItemID                    string
	FulfillmentAllocationID                 string
	SourceLocationCode, TargetLocationCode  string
	ExpectedQuantity, ProcessedQuantity     inventory.Quantity
	AssignedTo, ExceptionCode, CancelReason string
	Version                                 int64
	ClaimedAt, StartedAt, CompletedAt       *time.Time
	CreatedAt, UpdatedAt                    time.Time
}

// WMSTaskEvent is an immutable command/effect record for a WMS task.
type WMSTaskEvent struct {
	ID, TaskID, IdempotencyKey, Kind string
	BarcodeDigest, LocationCode      string
	Quantity                         inventory.Quantity
	ActorID, ReasonCode              string
	OccurredAt                       time.Time
}

// WMSTaskListOptions contains bounded tenant-local queue filters.
type WMSTaskListOptions struct {
	State, TaskType, WarehouseID, Cursor string
	Limit                                int
}

// CreateOrderPickTasks is the atomic order-to-allocation-to-pick command.
type CreateOrderPickTasks struct {
	OrderID, WarehouseID, IdempotencyKey string
}

// CreateWMSTask is a standalone provider-neutral warehouse execution command.
// It deliberately has no order or remote-provider fields.
type CreateWMSTask struct {
	ID, IdempotencyKey, TaskType, WarehouseID, SKU string
	SourceLocationCode, TargetLocationCode         string
	ExpectedQuantity                               inventory.Quantity
}

// WMSBatch is a bounded internal grouping of completed pick tasks for the
// local pack handoff. It is not a marketplace shipment.
type WMSBatch struct {
	ID, IdempotencyKey, Kind, State, WarehouseID string
	TaskIDs                                      []string
	Version                                      int64
	CreatedAt, UpdatedAt                         time.Time
}

// WMSBatchEvent is an immutable local batch command/effect record.
type WMSBatchEvent struct {
	ID, BatchID, IdempotencyKey, Kind, ActorID string
	OccurredAt                                 time.Time
}

// CreateWMSBatch groups completed pick tasks in one tenant-scoped transaction.
type CreateWMSBatch struct {
	ID, IdempotencyKey, WarehouseID string
	TaskIDs                         []string
}

const wmsTaskSelect = `SELECT task_id,idempotency_key,task_type,state,warehouse_id,sku,unit,order_id,order_item_id,fulfillment_allocation_id,source_location_code,target_location_code,expected_quantity_coefficient,expected_quantity_scale,processed_quantity_coefficient,processed_quantity_scale,assigned_to,exception_code,cancel_reason,version,claimed_at,started_at,completed_at,created_at,updated_at FROM wms_execution_tasks WHERE organization_id=$1 AND workspace_id=$2`
const wmsTaskEventSelect = `SELECT event_id,task_id,idempotency_key,kind,barcode_digest,location_code,quantity_coefficient,quantity_scale,unit,reason_code,actor_id,occurred_at FROM wms_execution_task_events WHERE organization_id=$1 AND workspace_id=$2`
const wmsBatchSelect = `SELECT batch_id,idempotency_key,kind,state,warehouse_id,version,created_at,updated_at FROM wms_execution_batches WHERE organization_id=$1 AND workspace_id=$2`

// ListWMSTasks returns a bounded newest-first tenant-scoped queue page.
func (r *Repository) ListWMSTasks(ctx context.Context, s inventory.Scope, options WMSTaskListOptions) ([]WMSTask, string, error) {
	if err := validate(ctx, r, s); err != nil {
		return nil, "", err
	}
	if options.Limit < 1 || options.Limit > 200 || !validWMSFilter(options.State, options.TaskType, options.WarehouseID) {
		return nil, "", inventory.ErrInvalidRecord
	}
	cursor, err := decodeWMSCursor(options.Cursor)
	if err != nil {
		return nil, "", inventory.ErrInvalidRecord
	}
	items := make([]WMSTask, 0, options.Limit)
	err = r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		args := []any{s.OrganizationID(), s.WorkspaceID()}
		where := ""
		if options.State != "" {
			args = append(args, options.State)
			where += fmt.Sprintf(" AND state=$%d", len(args))
		}
		if options.TaskType != "" {
			args = append(args, options.TaskType)
			where += fmt.Sprintf(" AND task_type=$%d", len(args))
		}
		if options.WarehouseID != "" {
			args = append(args, options.WarehouseID)
			where += fmt.Sprintf(" AND warehouse_id=$%d", len(args))
		}
		if cursor != nil {
			args = append(args, cursor.At, cursor.ID)
			where += fmt.Sprintf(" AND (updated_at<$%d OR (updated_at=$%d AND task_id<$%d))", len(args)-1, len(args)-1, len(args))
		}
		args = append(args, options.Limit)
		query := wmsTaskSelect + where + fmt.Sprintf(" ORDER BY updated_at DESC,task_id DESC LIMIT $%d", len(args))
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("inventory repository: list wms tasks: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanWMSTask(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, "", err
	}
	var next string
	if len(items) == options.Limit {
		last := items[len(items)-1]
		next = encodeWMSCursor(last.UpdatedAt, last.ID)
	}
	return items, next, nil
}

// WMSTask returns one task in the authenticated tenant scope.
func (r *Repository) WMSTask(ctx context.Context, s inventory.Scope, id string) (WMSTask, error) {
	if err := validate(ctx, r, s); err != nil || !validSortableIDValue(id) {
		return WMSTask{}, inventory.ErrInvalidRecord
	}
	var out WMSTask
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		var err error
		out, err = loadWMSTask(ctx, tx, s, id, false)
		return err
	})
	return out, err
}

// WMSCreateTask creates or replays one standalone warehouse task. It records
// the initial event and outbox message in the same transaction as the task.
func (r *Repository) WMSCreateTask(ctx context.Context, s inventory.Scope, command CreateWMSTask, mutation inventory.Mutation) (WMSTask, bool, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(command.ID) || !validTokenValue(command.IdempotencyKey) || !validWMSTaskType(command.TaskType) || !validSortableIDValue(command.WarehouseID) || !validTokenValue(command.SKU) || !validWMSStandaloneContext(command) || command.ExpectedQuantity.Validate() != nil || command.ExpectedQuantity.Value.IsZero() {
		return WMSTask{}, false, inventory.ErrInvalidRecord
	}
	var out WMSTask
	created := false
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		if err := requireWarehouseAllocatable(ctx, tx, s, inventory.WarehouseID(command.WarehouseID)); err != nil {
			return err
		}
		var existingID string
		err := tx.QueryRowContext(ctx, `SELECT task_id FROM wms_execution_tasks WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), command.IdempotencyKey).Scan(&existingID)
		if err == nil {
			existing, loadErr := loadWMSTask(ctx, tx, s, existingID, false)
			if loadErr != nil {
				return loadErr
			}
			if !sameStandaloneWMSTask(existing, command) {
				return inventory.ErrConflict
			}
			out = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		out = WMSTask{ID: command.ID, IdempotencyKey: command.IdempotencyKey, TaskType: command.TaskType, State: "pending", WarehouseID: command.WarehouseID, SKU: command.SKU, SourceLocationCode: command.SourceLocationCode, TargetLocationCode: command.TargetLocationCode, ExpectedQuantity: command.ExpectedQuantity, ProcessedQuantity: mustInventoryQuantity(0, command.ExpectedQuantity.Unit), Version: 1, CreatedAt: mutation.OccurredAt.UTC(), UpdatedAt: mutation.OccurredAt.UTC()}
		insert, insertErr := tx.ExecContext(ctx, `INSERT INTO wms_execution_tasks(organization_id,workspace_id,task_id,idempotency_key,task_type,state,warehouse_id,sku,unit,order_id,order_item_id,fulfillment_allocation_id,source_location_code,target_location_code,expected_quantity_coefficient,expected_quantity_scale,processed_quantity_coefficient,processed_quantity_scale,assigned_to,exception_code,cancel_reason,version,claimed_at,started_at,completed_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'pending',$6,$7,$8,NULL,NULL,NULL,$9,$10,$11,$12,0,0,'','','',1,$13,$13) ON CONFLICT (organization_id,workspace_id,idempotency_key) DO NOTHING`, s.OrganizationID(), s.WorkspaceID(), out.ID, out.IdempotencyKey, out.TaskType, out.WarehouseID, out.SKU, out.ExpectedQuantity.Unit.String(), out.SourceLocationCode, out.TargetLocationCode, out.ExpectedQuantity.Value.Coefficient(), out.ExpectedQuantity.Value.Scale(), out.CreatedAt)
		if insertErr != nil {
			return insertErr
		}
		rows, rowsErr := insert.RowsAffected()
		if rowsErr != nil {
			return rowsErr
		}
		if rows == 0 {
			if err := tx.QueryRowContext(ctx, `SELECT task_id FROM wms_execution_tasks WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), command.IdempotencyKey).Scan(&existingID); err != nil {
				return err
			}
			existing, loadErr := loadWMSTask(ctx, tx, s, existingID, false)
			if loadErr != nil {
				return loadErr
			}
			if !sameStandaloneWMSTask(existing, command) {
				return inventory.ErrConflict
			}
			out = existing
			return nil
		}
		created = true
		event := WMSTaskEvent{ID: mutation.EventID, TaskID: out.ID, IdempotencyKey: out.IdempotencyKey, Kind: "created", Quantity: mustInventoryQuantity(0, out.ExpectedQuantity.Unit), ActorID: mutation.ActorID, OccurredAt: mutation.OccurredAt.UTC()}
		if err := insertWMSTaskEvent(ctx, tx, s, event); err != nil {
			return err
		}
		if err := appendWMSTaskAudit(ctx, tx, s, mutation, out, "created", map[string]any{"standalone": true, "version": out.Version}); err != nil {
			return err
		}
		return enqueueWMSTaskEvent(ctx, tx, s, mutation, out, "created")
	})
	return out, created, err
}

// WMSClaimTask assigns a pending task to one operator.
func (r *Repository) WMSClaimTask(ctx context.Context, s inventory.Scope, id, actor, idempotencyKey string, expectedVersion int64, mutation inventory.Mutation) (WMSTask, error) {
	return r.mutateWMSTask(ctx, s, id, actor, idempotencyKey, expectedVersion, mutation, "claimed", func(task *WMSTask, at time.Time) error {
		if task.State != "pending" {
			return ErrWMSTaskState
		}
		if task.AssignedTo != "" && task.AssignedTo != actor {
			return ErrWMSTaskAssignment
		}
		if task.AssignedTo == "" {
			task.AssignedTo = actor
			task.ClaimedAt = timePtr(at)
		}
		return nil
	})
}

// WMSStartTask moves a claimed or unclaimed pending task into execution.
func (r *Repository) WMSStartTask(ctx context.Context, s inventory.Scope, id, actor, idempotencyKey string, expectedVersion int64, mutation inventory.Mutation) (WMSTask, error) {
	return r.mutateWMSTask(ctx, s, id, actor, idempotencyKey, expectedVersion, mutation, "started", func(task *WMSTask, at time.Time) error {
		if task.AssignedTo != "" && task.AssignedTo != actor {
			return ErrWMSTaskAssignment
		}
		switch task.State {
		case "pending":
			task.State = "in_progress"
			task.StartedAt = timePtr(at)
		case "in_progress":
			return nil
		default:
			return ErrWMSTaskState
		}
		return nil
	})
}

// WMSScanTask applies one exact-quantity scan and records only a barcode digest.
func (r *Repository) WMSScanTask(ctx context.Context, s inventory.Scope, id, actor, idempotencyKey, barcode, locationCode string, quantity inventory.Quantity, expectedVersion int64, mutation inventory.Mutation) (WMSTask, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(id) || !validTokenValue(idempotencyKey) || actor == "" || strings.TrimSpace(barcode) == "" || len(barcode) > 256 || len(locationCode) > 128 || expectedVersion < 1 || quantity.Validate() != nil || quantity.Value.IsZero() {
		return WMSTask{}, inventory.ErrInvalidRecord
	}
	var out WMSTask
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		if replay, err := replayWMSTaskEvent(ctx, tx, s, id, idempotencyKey); err != nil {
			return err
		} else if replay != nil {
			out = *replay
			return nil
		}
		current, err := loadWMSTask(ctx, tx, s, id, true)
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return inventory.ErrConflict
		}
		if current.AssignedTo != "" && current.AssignedTo != actor {
			return ErrWMSTaskAssignment
		}
		if current.State != "pending" && current.State != "in_progress" {
			return ErrWMSTaskState
		}
		if quantity.Unit != current.ExpectedQuantity.Unit {
			return inventory.ErrInvalidRecord
		}
		processed, err := current.ProcessedQuantity.Value.Add(quantity.Value)
		if err != nil {
			return inventory.ErrInvalidRecord
		}
		cmp, err := processed.Cmp(current.ExpectedQuantity.Value)
		if err != nil || cmp > 0 {
			return inventory.ErrInvalidRecord
		}
		newQuantity, err := inventory.NewQuantity(processed, quantity.Unit)
		if err != nil {
			return inventory.ErrInvalidRecord
		}
		state := "in_progress"
		var completed *time.Time
		if cmp == 0 {
			state = "completed"
			completed = timePtr(mutation.OccurredAt)
		}
		started := current.StartedAt
		if started == nil {
			started = timePtr(mutation.OccurredAt)
		}
		out, err = updateWMSTask(ctx, tx, s, current, state, newQuantity, current.AssignedTo, current.ExceptionCode, current.CancelReason, started, completed, mutation.OccurredAt)
		if err != nil {
			return err
		}
		hash := sha256.Sum256([]byte(barcode))
		event := WMSTaskEvent{ID: mutation.EventID, TaskID: id, IdempotencyKey: idempotencyKey, Kind: "scan_applied", BarcodeDigest: hex.EncodeToString(hash[:]), LocationCode: locationCode, Quantity: quantity, ActorID: actor, OccurredAt: mutation.OccurredAt.UTC()}
		if err := insertWMSTaskEvent(ctx, tx, s, event); err != nil {
			return err
		}
		if err := appendWMSTaskAudit(ctx, tx, s, mutation, out, "scan_applied", map[string]any{"barcode_digest": event.BarcodeDigest, "location_code": locationCode, "quantity": quantity.Value.String(), "version": out.Version}); err != nil {
			return err
		}
		return enqueueWMSTaskEvent(ctx, tx, s, mutation, out, "scan_applied")
	})
	return out, err
}

// WMSCompleteTask closes a task only after its expected quantity was scanned.
func (r *Repository) WMSCompleteTask(ctx context.Context, s inventory.Scope, id, actor, idempotencyKey string, expectedVersion int64, mutation inventory.Mutation) (WMSTask, error) {
	return r.mutateWMSTask(ctx, s, id, actor, idempotencyKey, expectedVersion, mutation, "completed", func(task *WMSTask, _ time.Time) error {
		if task.AssignedTo != "" && task.AssignedTo != actor {
			return ErrWMSTaskAssignment
		}
		if task.State != "in_progress" {
			return ErrWMSTaskState
		}
		cmp, err := task.ProcessedQuantity.Value.Cmp(task.ExpectedQuantity.Value)
		if err != nil || cmp != 0 {
			return inventory.ErrInvalidRecord
		}
		task.State = "completed"
		task.CompletedAt = timePtr(mutation.OccurredAt)
		return nil
	})
}

// WMSExceptionTask places an unfinished task in manual attention state.
func (r *Repository) WMSExceptionTask(ctx context.Context, s inventory.Scope, id, actor, idempotencyKey, reasonCode string, expectedVersion int64, mutation inventory.Mutation) (WMSTask, error) {
	return r.mutateWMSTask(ctx, s, id, actor, idempotencyKey, expectedVersion, mutation, "exception", func(task *WMSTask, _ time.Time) error {
		if task.AssignedTo != "" && task.AssignedTo != actor {
			return ErrWMSTaskAssignment
		}
		if task.State != "pending" && task.State != "in_progress" {
			return ErrWMSTaskState
		}
		if !validReasonValue(reasonCode) {
			return inventory.ErrInvalidRecord
		}
		task.State, task.ExceptionCode = "exception", reasonCode
		return nil
	})
}

// WMSCancelTask cancels an unfinished task while retaining its history.
func (r *Repository) WMSCancelTask(ctx context.Context, s inventory.Scope, id, actor, idempotencyKey, reasonCode string, expectedVersion int64, mutation inventory.Mutation) (WMSTask, error) {
	return r.mutateWMSTask(ctx, s, id, actor, idempotencyKey, expectedVersion, mutation, "cancelled", func(task *WMSTask, _ time.Time) error {
		if task.AssignedTo != "" && task.AssignedTo != actor {
			return ErrWMSTaskAssignment
		}
		if task.State != "pending" && task.State != "in_progress" && task.State != "exception" {
			return ErrWMSTaskState
		}
		if !validReasonValue(reasonCode) {
			return inventory.ErrInvalidRecord
		}
		task.State, task.CancelReason = "cancelled", reasonCode
		return nil
	})
}

func (r *Repository) mutateWMSTask(ctx context.Context, s inventory.Scope, id, actor, idempotencyKey string, expectedVersion int64, mutation inventory.Mutation, change string, apply func(*WMSTask, time.Time) error) (WMSTask, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(id) || !validTokenValue(idempotencyKey) || actor == "" || expectedVersion < 1 {
		return WMSTask{}, inventory.ErrInvalidRecord
	}
	var out WMSTask
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		if replay, err := replayWMSTaskEvent(ctx, tx, s, id, idempotencyKey); err != nil {
			return err
		} else if replay != nil {
			out = *replay
			return nil
		}
		current, err := loadWMSTask(ctx, tx, s, id, true)
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return inventory.ErrConflict
		}
		if err := apply(&current, mutation.OccurredAt.UTC()); err != nil {
			return err
		}
		out, err = updateWMSTask(ctx, tx, s, current, current.State, current.ProcessedQuantity, current.AssignedTo, current.ExceptionCode, current.CancelReason, current.StartedAt, current.CompletedAt, mutation.OccurredAt)
		if err != nil {
			return err
		}
		kind := change
		event := WMSTaskEvent{ID: mutation.EventID, TaskID: id, IdempotencyKey: idempotencyKey, Kind: kind, Quantity: mustInventoryQuantity(0, current.ExpectedQuantity.Unit), ActorID: actor, ReasonCode: firstNonEmpty(current.ExceptionCode, current.CancelReason), OccurredAt: mutation.OccurredAt.UTC()}
		if err := insertWMSTaskEvent(ctx, tx, s, event); err != nil {
			return err
		}
		if err := appendWMSTaskAudit(ctx, tx, s, mutation, out, change, map[string]any{"version": out.Version, "reason_code": event.ReasonCode}); err != nil {
			return err
		}
		return enqueueWMSTaskEvent(ctx, tx, s, mutation, out, change)
	})
	return out, err
}

// WMSCreateOrderPickTasks reserves every order item and creates one pick task
// per item in one transaction. It is replay-safe through derived item keys.
func (r *Repository) WMSCreateOrderPickTasks(ctx context.Context, s inventory.Scope, command CreateOrderPickTasks, mutation inventory.Mutation) ([]WMSTask, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(command.OrderID) || !validSortableIDValue(command.WarehouseID) || !validTokenValue(command.IdempotencyKey) {
		return nil, inventory.ErrInvalidRecord
	}
	out := make([]WMSTask, 0)
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		if err := requireWarehouseAllocatable(ctx, tx, s, inventory.WarehouseID(command.WarehouseID)); err != nil {
			return err
		}
		var orderStatus string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM orders WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`, s.OrganizationID(), s.WorkspaceID(), command.OrderID).Scan(&orderStatus); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return inventory.ErrNotFound
			}
			return err
		}
		if orderStatus == "fulfilled" || orderStatus == "cancelled" {
			return inventory.ErrInvalidRecord
		}
		rows, err := tx.QueryContext(ctx, `SELECT id,sku_snapshot,quantity_coefficient,quantity_scale,unit,offer_id FROM order_items WHERE organization_id=$1 AND workspace_id=$2 AND order_id=$3 ORDER BY position,id`, s.OrganizationID(), s.WorkspaceID(), command.OrderID)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var itemID, sku, unit, offerID string
			var coefficient int64
			var scale int16
			if err := rows.Scan(&itemID, &sku, &coefficient, &scale, &unit, &offerID); err != nil {
				return err
			}
			count++
			decimal, err := inventory.NewDecimal(coefficient, uint8(scale))
			if err != nil {
				return inventory.ErrInvalidRecord
			}
			quantity, err := inventory.NewQuantity(decimal, inventory.UnitCode(unit))
			if err != nil || sku == "" {
				return inventory.ErrInvalidRecord
			}
			taskKey := wmsDerivedKey("wms_task", command.IdempotencyKey, itemID)
			taskID := wmsDerivedUUID(command.IdempotencyKey, itemID, "task")
			var task WMSTask
			targets := newWMSTaskScanner(&task)
			err = tx.QueryRowContext(ctx, wmsTaskSelect+` AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), taskKey).Scan(targets.targets()...)
			if err == nil {
				if err := targets.finish(); err != nil {
					return inventory.ErrInvalidRecord
				}
				if task.OrderID != command.OrderID || task.OrderItemID != itemID || task.WarehouseID != command.WarehouseID || task.TaskType != "pick" {
					return inventory.ErrConflict
				}
				out = append(out, task)
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			allocation, err := activeAllocationForItemTx(ctx, tx, s, itemID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil {
				cmp, quantityErr := allocation.Quantity.Value.Cmp(quantity.Value)
				if allocation.WarehouseID.String() != command.WarehouseID || allocation.OrderID != command.OrderID || allocation.OfferID.String() != offerID || allocation.Quantity.Unit != quantity.Unit || quantityErr != nil || cmp != 0 {
					return inventory.ErrConflict
				}
			} else {
				allocationMutation := derivedWMSMutation(mutation, itemID, "allocation")
				allocation, err = reserveOrderItemTx(ctx, tx, s, inventory.ReserveOrderItem{AllocationID: wmsDerivedUUID(command.IdempotencyKey, itemID, "allocation"), OrderItemID: itemID, IdempotencyKey: wmsDerivedKey("wms_allocation", command.IdempotencyKey, itemID), WarehouseID: inventory.WarehouseID(command.WarehouseID)}, allocationMutation)
				if err != nil {
					return err
				}
			}
			created := WMSTask{ID: taskID, IdempotencyKey: taskKey, TaskType: "pick", State: "pending", WarehouseID: command.WarehouseID, SKU: sku, OrderID: command.OrderID, OrderItemID: itemID, FulfillmentAllocationID: allocation.ID, ExpectedQuantity: quantity, ProcessedQuantity: mustInventoryQuantity(0, quantity.Unit), Version: 1, CreatedAt: mutation.OccurredAt.UTC(), UpdatedAt: mutation.OccurredAt.UTC()}
			if err := insertWMSTask(ctx, tx, s, created); err != nil {
				return err
			}
			taskMutation := derivedWMSMutation(mutation, itemID, "task")
			event := WMSTaskEvent{ID: taskMutation.EventID, TaskID: taskID, IdempotencyKey: taskKey, Kind: "created", Quantity: mustInventoryQuantity(0, quantity.Unit), ActorID: taskMutation.ActorID, OccurredAt: taskMutation.OccurredAt}
			if err := insertWMSTaskEvent(ctx, tx, s, event); err != nil {
				return err
			}
			if err := appendWMSTaskAudit(ctx, tx, s, taskMutation, created, "created", map[string]any{"order_id": command.OrderID, "order_item_id": itemID, "fulfillment_allocation_id": allocation.ID, "version": 1}); err != nil {
				return err
			}
			if err := enqueueWMSTaskEvent(ctx, tx, s, taskMutation, created, "created"); err != nil {
				return err
			}
			out = append(out, created)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count == 0 {
			return inventory.ErrInvalidRecord
		}
		return nil
	})
	return out, err
}

// WMSCreateBatch groups completed pick tasks into one local pack handoff. The
// operation is bounded to 50 tasks and is idempotent by the caller key.
func (r *Repository) WMSCreateBatch(ctx context.Context, s inventory.Scope, command CreateWMSBatch, mutation inventory.Mutation) (WMSBatch, bool, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(command.ID) || !validTokenValue(command.IdempotencyKey) || !validSortableIDValue(command.WarehouseID) || !validWMSBatchTaskIDs(command.TaskIDs) {
		return WMSBatch{}, false, inventory.ErrInvalidRecord
	}
	var out WMSBatch
	created := false
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		if err := requireWarehouseAllocatable(ctx, tx, s, inventory.WarehouseID(command.WarehouseID)); err != nil {
			return err
		}
		var existingID string
		err := tx.QueryRowContext(ctx, `SELECT batch_id FROM wms_execution_batches WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), command.IdempotencyKey).Scan(&existingID)
		if err == nil {
			existing, loadErr := loadWMSBatch(ctx, tx, s, existingID, false)
			if loadErr != nil {
				return loadErr
			}
			if !sameWMSBatch(existing, command) {
				return inventory.ErrConflict
			}
			out = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		placeholders := make([]string, len(command.TaskIDs))
		args := []any{s.OrganizationID(), s.WorkspaceID()}
		for index, taskID := range command.TaskIDs {
			placeholders[index] = fmt.Sprintf("$%d", len(args)+1)
			args = append(args, taskID)
		}
		rows, queryErr := tx.QueryContext(ctx, `SELECT task_id,task_type,state,warehouse_id FROM wms_execution_tasks WHERE organization_id=$1 AND workspace_id=$2 AND task_id IN (`+strings.Join(placeholders, ",")+") FOR UPDATE", args...)
		if queryErr != nil {
			return queryErr
		}
		type taskReference struct{ Type, State, WarehouseID string }
		found := make(map[string]taskReference, len(command.TaskIDs))
		for rows.Next() {
			var id string
			var ref taskReference
			if err := rows.Scan(&id, &ref.Type, &ref.State, &ref.WarehouseID); err != nil {
				_ = rows.Close()
				return err
			}
			found[id] = ref
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, taskID := range command.TaskIDs {
			ref, ok := found[taskID]
			if !ok {
				return inventory.ErrNotFound
			}
			if ref.Type != "pick" || ref.State != "completed" || ref.WarehouseID != command.WarehouseID {
				return ErrWMSBatchState
			}
			var existingBatchID string
			err := tx.QueryRowContext(ctx, `SELECT batch_id FROM wms_execution_batch_tasks WHERE organization_id=$1 AND workspace_id=$2 AND task_id=$3`, s.OrganizationID(), s.WorkspaceID(), taskID).Scan(&existingBatchID)
			if err == nil {
				return inventory.ErrConflict
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		out = WMSBatch{ID: command.ID, IdempotencyKey: command.IdempotencyKey, Kind: "pack_handoff", State: "ready", WarehouseID: command.WarehouseID, TaskIDs: append([]string(nil), command.TaskIDs...), Version: 1, CreatedAt: mutation.OccurredAt.UTC(), UpdatedAt: mutation.OccurredAt.UTC()}
		if _, err := tx.ExecContext(ctx, `INSERT INTO wms_execution_batches(organization_id,workspace_id,batch_id,idempotency_key,kind,state,warehouse_id,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,$8)`, s.OrganizationID(), s.WorkspaceID(), out.ID, out.IdempotencyKey, out.Kind, out.State, out.WarehouseID, out.CreatedAt); err != nil {
			return err
		}
		for index, taskID := range command.TaskIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO wms_execution_batch_tasks(organization_id,workspace_id,batch_id,task_id,position) VALUES($1,$2,$3,$4,$5)`, s.OrganizationID(), s.WorkspaceID(), out.ID, taskID, index+1); err != nil {
				return err
			}
		}
		created = true
		event := WMSBatchEvent{ID: mutation.EventID, BatchID: out.ID, IdempotencyKey: out.IdempotencyKey, Kind: "created", ActorID: mutation.ActorID, OccurredAt: mutation.OccurredAt.UTC()}
		if err := insertWMSBatchEvent(ctx, tx, s, event); err != nil {
			return err
		}
		if err := appendWMSBatchAudit(ctx, tx, s, mutation, out, "created"); err != nil {
			return err
		}
		return enqueueWMSBatchEvent(ctx, tx, s, mutation, out, "created")
	})
	return out, created, err
}

// WMSBatch returns one tenant-scoped local pack handoff batch.
func (r *Repository) WMSBatch(ctx context.Context, s inventory.Scope, id string) (WMSBatch, error) {
	if err := validate(ctx, r, s); err != nil || !validSortableIDValue(id) {
		return WMSBatch{}, inventory.ErrInvalidRecord
	}
	var out WMSBatch
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		var err error
		out, err = loadWMSBatch(ctx, tx, s, id, false)
		return err
	})
	return out, err
}

// WMSHandoffBatch marks a ready local batch as handed off to the pack work
// area. No external shipment or on-hand mutation is performed.
func (r *Repository) WMSHandoffBatch(ctx context.Context, s inventory.Scope, id, actor, idempotencyKey string, expectedVersion int64, mutation inventory.Mutation) (WMSBatch, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(id) || !validTokenValue(idempotencyKey) || actor == "" || expectedVersion < 1 {
		return WMSBatch{}, inventory.ErrInvalidRecord
	}
	var out WMSBatch
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		if replay, err := replayWMSBatchEvent(ctx, tx, s, id, idempotencyKey); err != nil {
			return err
		} else if replay != nil {
			out = *replay
			return nil
		}
		current, err := loadWMSBatch(ctx, tx, s, id, true)
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return inventory.ErrConflict
		}
		if current.State != "ready" {
			return ErrWMSBatchState
		}
		if err := tx.QueryRowContext(ctx, `UPDATE wms_execution_batches SET state='handed_off',version=version+1,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND batch_id=$3 AND version=$5 RETURNING batch_id,idempotency_key,kind,state,warehouse_id,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), id, mutation.OccurredAt.UTC(), current.Version).Scan(&out.ID, &out.IdempotencyKey, &out.Kind, &out.State, &out.WarehouseID, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return inventory.ErrConflict
			}
			return err
		}
		out.CreatedAt, out.UpdatedAt = out.CreatedAt.UTC(), out.UpdatedAt.UTC()
		out.TaskIDs, err = loadWMSBatchTaskIDs(ctx, tx, s, id)
		if err != nil {
			return err
		}
		event := WMSBatchEvent{ID: mutation.EventID, BatchID: id, IdempotencyKey: idempotencyKey, Kind: "handed_off", ActorID: actor, OccurredAt: mutation.OccurredAt.UTC()}
		if err := insertWMSBatchEvent(ctx, tx, s, event); err != nil {
			return err
		}
		if err := appendWMSBatchAudit(ctx, tx, s, mutation, out, "handed_off"); err != nil {
			return err
		}
		return enqueueWMSBatchEvent(ctx, tx, s, mutation, out, "handed_off")
	})
	return out, err
}

func loadWMSBatch(ctx context.Context, tx *sql.Tx, s inventory.Scope, id string, lock bool) (WMSBatch, error) {
	query := wmsBatchSelect + ` AND batch_id=$3`
	if lock {
		query += ` FOR UPDATE`
	}
	var out WMSBatch
	if err := tx.QueryRowContext(ctx, query, s.OrganizationID(), s.WorkspaceID(), id).Scan(&out.ID, &out.IdempotencyKey, &out.Kind, &out.State, &out.WarehouseID, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WMSBatch{}, inventory.ErrNotFound
		}
		return WMSBatch{}, err
	}
	var err error
	out.TaskIDs, err = loadWMSBatchTaskIDs(ctx, tx, s, id)
	if err != nil {
		return WMSBatch{}, err
	}
	out.CreatedAt, out.UpdatedAt = out.CreatedAt.UTC(), out.UpdatedAt.UTC()
	return out, nil
}

func loadWMSBatchTaskIDs(ctx context.Context, tx *sql.Tx, s inventory.Scope, id string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT task_id FROM wms_execution_batch_tasks WHERE organization_id=$1 AND workspace_id=$2 AND batch_id=$3 ORDER BY position`, s.OrganizationID(), s.WorkspaceID(), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]string, 0, 50)
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return nil, err
		}
		items = append(items, taskID)
	}
	return items, rows.Err()
}

func sameStandaloneWMSTask(existing WMSTask, command CreateWMSTask) bool {
	return existing.TaskType == command.TaskType && existing.WarehouseID == command.WarehouseID && existing.SKU == command.SKU && existing.SourceLocationCode == command.SourceLocationCode && existing.TargetLocationCode == command.TargetLocationCode && sameWMSQuantity(existing.ExpectedQuantity, command.ExpectedQuantity)
}

func sameWMSQuantity(left, right inventory.Quantity) bool {
	if left.Unit != right.Unit {
		return false
	}
	cmp, err := left.Value.Cmp(right.Value)
	return err == nil && cmp == 0
}

func sameWMSBatch(existing WMSBatch, command CreateWMSBatch) bool {
	if existing.WarehouseID != command.WarehouseID || len(existing.TaskIDs) != len(command.TaskIDs) {
		return false
	}
	for index := range command.TaskIDs {
		if existing.TaskIDs[index] != command.TaskIDs[index] {
			return false
		}
	}
	return true
}

func validWMSTaskType(value string) bool {
	switch value {
	case "receiving", "put_away", "pick", "pack", "cycle_count", "transfer", "return_receiving":
		return true
	default:
		return false
	}
}

func validWMSLocation(value string) bool {
	return value == "" || (len(value) <= 128 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00"))
}

func validWMSStandaloneContext(command CreateWMSTask) bool {
	if !validWMSLocation(command.SourceLocationCode) || !validWMSLocation(command.TargetLocationCode) {
		return false
	}
	switch command.TaskType {
	case "receiving", "return_receiving":
		return command.TargetLocationCode != ""
	case "put_away", "transfer":
		return command.SourceLocationCode != "" && command.TargetLocationCode != "" && command.SourceLocationCode != command.TargetLocationCode
	case "cycle_count", "pick", "pack":
		return command.SourceLocationCode != ""
	default:
		return false
	}
}

func validWMSBatchTaskIDs(values []string) bool {
	if len(values) < 1 || len(values) > 50 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validSortableIDValue(value) {
			return false
		}
		if _, ok := seen[value]; ok {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

// WMSHistory returns immutable task command history.
func (r *Repository) WMSHistory(ctx context.Context, s inventory.Scope, id string, limit int) ([]WMSTaskEvent, error) {
	if err := validate(ctx, r, s); err != nil || !validSortableIDValue(id) || limit < 1 || limit > 200 {
		return nil, inventory.ErrInvalidRecord
	}
	items := make([]WMSTaskEvent, 0, limit)
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		if _, err := loadWMSTask(ctx, tx, s, id, false); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, wmsTaskEventSelect+` AND task_id=$3 ORDER BY occurred_at DESC,event_id DESC LIMIT $4`, s.OrganizationID(), s.WorkspaceID(), id, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanWMSTaskEvent(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

func validWMSFilter(state, taskType, warehouse string) bool {
	if state != "" && state != "pending" && state != "in_progress" && state != "completed" && state != "cancelled" && state != "exception" {
		return false
	}
	if taskType != "" && taskType != "receiving" && taskType != "put_away" && taskType != "pick" && taskType != "pack" && taskType != "cycle_count" && taskType != "transfer" && taskType != "return_receiving" {
		return false
	}
	return warehouse == "" || validSortableIDValue(warehouse)
}

type wmsCursor struct {
	At time.Time
	ID string
}

func encodeWMSCursor(at time.Time, id string) string {
	value := at.UTC().Format(time.RFC3339Nano) + "|" + id
	return "v1." + base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeWMSCursor(value string) (*wmsCursor, error) {
	if value == "" {
		return nil, nil
	}
	if !strings.HasPrefix(value, "v1.") {
		return nil, errors.New("invalid wms cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "v1."))
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 2 || !validSortableIDValue(parts[1]) {
		return nil, errors.New("invalid wms cursor")
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil || at.Location() != time.UTC {
		return nil, errors.New("invalid wms cursor")
	}
	return &wmsCursor{At: at, ID: parts[1]}, nil
}

func loadWMSTask(ctx context.Context, tx *sql.Tx, s inventory.Scope, id string, lock bool) (WMSTask, error) {
	query := wmsTaskSelect + ` AND task_id=$3`
	if lock {
		query += ` FOR UPDATE`
	}
	var out WMSTask
	targets := newWMSTaskScanner(&out)
	err := tx.QueryRowContext(ctx, query, s.OrganizationID(), s.WorkspaceID(), id).Scan(targets.targets()...)
	if errors.Is(err, sql.ErrNoRows) {
		return WMSTask{}, inventory.ErrNotFound
	}
	if err != nil {
		return WMSTask{}, err
	}
	if err := targets.finish(); err != nil {
		return WMSTask{}, inventory.ErrInvalidRecord
	}
	return out, nil
}

func scanWMSTask(row scanner) (WMSTask, error) {
	var out WMSTask
	targets := newWMSTaskScanner(&out)
	if err := row.Scan(targets.targets()...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WMSTask{}, inventory.ErrNotFound
		}
		return WMSTask{}, err
	}
	if err := targets.finish(); err != nil {
		return WMSTask{}, inventory.ErrInvalidRecord
	}
	if !validSortableIDValue(out.ID) || !validSortableIDValue(out.WarehouseID) || out.TaskType == "" || out.State == "" || out.Version < 1 {
		return WMSTask{}, inventory.ErrInvalidRecord
	}
	return out, nil
}

type wmsTaskScanner struct {
	out                                       *WMSTask
	orderID, orderItemID, allocationID        sql.NullString
	unit                                      string
	expectedCoefficient, processedCoefficient int64
	expectedScale, processedScale             int16
	claimedAt, startedAt, completedAt         sql.NullTime
}

func newWMSTaskScanner(out *WMSTask) *wmsTaskScanner { return &wmsTaskScanner{out: out} }

func (t *wmsTaskScanner) targets() []any {
	return []any{&t.out.ID, &t.out.IdempotencyKey, &t.out.TaskType, &t.out.State, &t.out.WarehouseID, &t.out.SKU, &t.unit, &t.orderID, &t.orderItemID, &t.allocationID, &t.out.SourceLocationCode, &t.out.TargetLocationCode, &t.expectedCoefficient, &t.expectedScale, &t.processedCoefficient, &t.processedScale, &t.out.AssignedTo, &t.out.ExceptionCode, &t.out.CancelReason, &t.out.Version, &t.claimedAt, &t.startedAt, &t.completedAt, &t.out.CreatedAt, &t.out.UpdatedAt}
}

func (t *wmsTaskScanner) finish() error {
	if t.orderID.Valid {
		t.out.OrderID = t.orderID.String
	}
	if t.orderItemID.Valid {
		t.out.OrderItemID = t.orderItemID.String
	}
	if t.allocationID.Valid {
		t.out.FulfillmentAllocationID = t.allocationID.String
	}
	expected, err := inventory.NewDecimal(t.expectedCoefficient, uint8(t.expectedScale))
	if err != nil {
		return err
	}
	processed, err := inventory.NewDecimal(t.processedCoefficient, uint8(t.processedScale))
	if err != nil {
		return err
	}
	t.out.ExpectedQuantity, err = inventory.NewQuantity(expected, inventory.UnitCode(t.unit))
	if err != nil {
		return err
	}
	t.out.ProcessedQuantity, err = inventory.NewQuantity(processed, t.out.ExpectedQuantity.Unit)
	if err != nil {
		return err
	}
	if t.claimedAt.Valid {
		v := t.claimedAt.Time.UTC()
		t.out.ClaimedAt = &v
	}
	if t.startedAt.Valid {
		v := t.startedAt.Time.UTC()
		t.out.StartedAt = &v
	}
	if t.completedAt.Valid {
		v := t.completedAt.Time.UTC()
		t.out.CompletedAt = &v
	}
	t.out.CreatedAt, t.out.UpdatedAt = t.out.CreatedAt.UTC(), t.out.UpdatedAt.UTC()
	return nil
}

func updateWMSTask(ctx context.Context, tx *sql.Tx, s inventory.Scope, current WMSTask, state string, processed inventory.Quantity, assigned, exceptionCode, cancelReason string, startedAt, completedAt *time.Time, at time.Time) (WMSTask, error) {
	var completed *time.Time
	if completedAt != nil {
		value := completedAt.UTC()
		completed = &value
	}
	var out WMSTask
	targets := newWMSTaskScanner(&out)
	err := tx.QueryRowContext(ctx, `UPDATE wms_execution_tasks SET state=$4,processed_quantity_coefficient=$5,processed_quantity_scale=$6,assigned_to=$7,exception_code=$8,cancel_reason=$9,started_at=$10,completed_at=$11,version=version+1,updated_at=$12 WHERE organization_id=$1 AND workspace_id=$2 AND task_id=$3 AND version=$13 RETURNING task_id,idempotency_key,task_type,state,warehouse_id,sku,unit,order_id,order_item_id,fulfillment_allocation_id,source_location_code,target_location_code,expected_quantity_coefficient,expected_quantity_scale,processed_quantity_coefficient,processed_quantity_scale,assigned_to,exception_code,cancel_reason,version,claimed_at,started_at,completed_at,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), current.ID, state, processed.Value.Coefficient(), processed.Value.Scale(), assigned, exceptionCode, cancelReason, nullableTime(startedAt), completed, at.UTC(), current.Version).Scan(targets.targets()...)
	if errors.Is(err, sql.ErrNoRows) {
		return WMSTask{}, inventory.ErrConflict
	}
	if err != nil {
		return WMSTask{}, err
	}
	if err := targets.finish(); err != nil {
		return WMSTask{}, inventory.ErrInvalidRecord
	}
	return out, nil
}

func insertWMSTask(ctx context.Context, tx *sql.Tx, s inventory.Scope, task WMSTask) error {
	if task.ExpectedQuantity.Validate() != nil || task.ProcessedQuantity.Validate() != nil {
		return inventory.ErrInvalidRecord
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO wms_execution_tasks(organization_id,workspace_id,task_id,idempotency_key,task_type,state,warehouse_id,sku,unit,order_id,order_item_id,fulfillment_allocation_id,source_location_code,target_location_code,expected_quantity_coefficient,expected_quantity_scale,processed_quantity_coefficient,processed_quantity_scale,assigned_to,exception_code,cancel_reason,version,claimed_at,started_at,completed_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)`, s.OrganizationID(), s.WorkspaceID(), task.ID, task.IdempotencyKey, task.TaskType, task.State, task.WarehouseID, task.SKU, task.ExpectedQuantity.Unit.String(), task.OrderID, task.OrderItemID, task.FulfillmentAllocationID, task.SourceLocationCode, task.TargetLocationCode, task.ExpectedQuantity.Value.Coefficient(), task.ExpectedQuantity.Value.Scale(), task.ProcessedQuantity.Value.Coefficient(), task.ProcessedQuantity.Value.Scale(), task.AssignedTo, task.ExceptionCode, task.CancelReason, task.Version, nullableTime(task.ClaimedAt), nullableTime(task.StartedAt), nullableTime(task.CompletedAt), task.CreatedAt.UTC(), task.UpdatedAt.UTC())
	return err
}

func replayWMSTaskEvent(ctx context.Context, tx *sql.Tx, s inventory.Scope, taskID, idempotencyKey string) (*WMSTask, error) {
	var existingTaskID string
	err := tx.QueryRowContext(ctx, `SELECT task_id FROM wms_execution_task_events WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), idempotencyKey).Scan(&existingTaskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if existingTaskID != taskID {
		return nil, inventory.ErrConflict
	}
	task, err := loadWMSTask(ctx, tx, s, taskID, false)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func insertWMSTaskEvent(ctx context.Context, tx *sql.Tx, s inventory.Scope, event WMSTaskEvent) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO wms_execution_task_events(organization_id,workspace_id,event_id,task_id,idempotency_key,kind,barcode_digest,location_code,quantity_coefficient,quantity_scale,unit,reason_code,actor_id,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, s.OrganizationID(), s.WorkspaceID(), event.ID, event.TaskID, event.IdempotencyKey, event.Kind, event.BarcodeDigest, event.LocationCode, event.Quantity.Value.Coefficient(), event.Quantity.Value.Scale(), event.Quantity.Unit.String(), event.ReasonCode, event.ActorID, event.OccurredAt.UTC())
	return err
}

func scanWMSTaskEvent(row scanner) (WMSTaskEvent, error) {
	var out WMSTaskEvent
	var coefficient int64
	var scale int16
	if err := row.Scan(&out.ID, &out.TaskID, &out.IdempotencyKey, &out.Kind, &out.BarcodeDigest, &out.LocationCode, &coefficient, &scale, &out.Quantity.Unit, &out.ReasonCode, &out.ActorID, &out.OccurredAt); err != nil {
		return WMSTaskEvent{}, err
	}
	decimal, err := inventory.NewDecimal(coefficient, uint8(scale))
	if err != nil {
		return WMSTaskEvent{}, inventory.ErrInvalidRecord
	}
	out.Quantity, err = inventory.NewQuantity(decimal, out.Quantity.Unit)
	if err != nil {
		return WMSTaskEvent{}, inventory.ErrInvalidRecord
	}
	out.OccurredAt = out.OccurredAt.UTC()
	return out, nil
}

func appendWMSTaskAudit(ctx context.Context, tx *sql.Tx, s inventory.Scope, mutation inventory.Mutation, task WMSTask, action string, extra map[string]any) error {
	summary := audit.Summary{"task_id": task.ID, "task_type": task.TaskType, "state": task.State, "warehouse_id": task.WarehouseID, "sku": task.SKU, "version": task.Version, "expected_quantity": task.ExpectedQuantity.Value.String(), "processed_quantity": task.ProcessedQuantity.Value.String()}
	for key, value := range extra {
		summary[key] = value
	}
	return appendAudit(ctx, tx, s, mutation, "wms_execution_task."+action, "wms_execution_task", task.ID, audit.RiskWriteSafe, summary)
}

func enqueueWMSTaskEvent(ctx context.Context, tx *sql.Tx, s inventory.Scope, mutation inventory.Mutation, task WMSTask, change string) error {
	payload := struct {
		TaskID                  string          `json:"task_id"`
		TaskType                string          `json:"task_type"`
		State                   string          `json:"state"`
		WarehouseID             string          `json:"warehouse_id"`
		SKU                     string          `json:"sku"`
		OrderID                 string          `json:"order_id,omitempty"`
		OrderItemID             string          `json:"order_item_id,omitempty"`
		FulfillmentAllocationID string          `json:"fulfillment_allocation_id,omitempty"`
		ExpectedQuantity        wmsWireQuantity `json:"expected_quantity"`
		ProcessedQuantity       wmsWireQuantity `json:"processed_quantity"`
		AssignedTo              string          `json:"assigned_to,omitempty"`
		Version                 int64           `json:"version"`
		Change                  string          `json:"change"`
	}{task.ID, task.TaskType, task.State, task.WarehouseID, task.SKU, task.OrderID, task.OrderItemID, task.FulfillmentAllocationID, wmsWireQuantity{task.ExpectedQuantity.Value.String(), task.ExpectedQuantity.Unit.String()}, wmsWireQuantity{task.ProcessedQuantity.Value.String(), task.ProcessedQuantity.Unit.String()}, task.AssignedTo, task.Version, change}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ev, err := eventBase(s, mutation, "commerce.fulfillment.task_changed.v1", "wms_execution_task", task.ID, data)
	if err != nil {
		return err
	}
	return enqueue(ctx, tx, ev)
}

func insertWMSBatchEvent(ctx context.Context, tx *sql.Tx, s inventory.Scope, event WMSBatchEvent) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO wms_execution_batch_events(organization_id,workspace_id,event_id,batch_id,idempotency_key,kind,actor_id,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, s.OrganizationID(), s.WorkspaceID(), event.ID, event.BatchID, event.IdempotencyKey, event.Kind, event.ActorID, event.OccurredAt.UTC())
	return err
}

func replayWMSBatchEvent(ctx context.Context, tx *sql.Tx, s inventory.Scope, batchID, idempotencyKey string) (*WMSBatch, error) {
	var existingBatchID string
	err := tx.QueryRowContext(ctx, `SELECT batch_id FROM wms_execution_batch_events WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), idempotencyKey).Scan(&existingBatchID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if existingBatchID != batchID {
		return nil, inventory.ErrConflict
	}
	out, err := loadWMSBatch(ctx, tx, s, batchID, false)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func appendWMSBatchAudit(ctx context.Context, tx *sql.Tx, s inventory.Scope, mutation inventory.Mutation, batch WMSBatch, action string) error {
	summary := audit.Summary{"batch_id": batch.ID, "kind": batch.Kind, "state": batch.State, "warehouse_id": batch.WarehouseID, "task_ids": batch.TaskIDs, "version": batch.Version}
	return appendAudit(ctx, tx, s, mutation, "wms_execution_batch."+action, "wms_execution_batch", batch.ID, audit.RiskWriteSafe, summary)
}

func enqueueWMSBatchEvent(ctx context.Context, tx *sql.Tx, s inventory.Scope, mutation inventory.Mutation, batch WMSBatch, change string) error {
	payload := struct {
		BatchID     string   `json:"batch_id"`
		Kind        string   `json:"kind"`
		State       string   `json:"state"`
		WarehouseID string   `json:"warehouse_id"`
		TaskIDs     []string `json:"task_ids"`
		Version     int64    `json:"version"`
		Change      string   `json:"change"`
	}{batch.ID, batch.Kind, batch.State, batch.WarehouseID, batch.TaskIDs, batch.Version, change}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ev, err := eventBase(s, mutation, "commerce.fulfillment.batch_changed.v1", "wms_execution_batch", batch.ID, data)
	if err != nil {
		return err
	}
	return enqueue(ctx, tx, ev)
}

type wmsWireQuantity struct {
	Value string `json:"value"`
	Unit  string `json:"unit"`
}

func activeAllocationForItemTx(ctx context.Context, tx *sql.Tx, s inventory.Scope, itemID string) (inventory.FulfillmentAllocation, error) {
	var out inventory.FulfillmentAllocation
	err := scanAllocation(tx.QueryRowContext(ctx, allocationSelect+` AND order_item_id=$3 AND state='reserved' FOR UPDATE`, s.OrganizationID(), s.WorkspaceID(), itemID), &out)
	return out, err
}

func reserveOrderItemTx(ctx context.Context, tx *sql.Tx, s inventory.Scope, command inventory.ReserveOrderItem, mutation inventory.Mutation) (inventory.FulfillmentAllocation, error) {
	if command.Validate() != nil {
		return inventory.FulfillmentAllocation{}, inventory.ErrInvalidRecord
	}
	var existing inventory.FulfillmentAllocation
	err := scanAllocation(tx.QueryRowContext(ctx, allocationSelect+` AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), command.IdempotencyKey), &existing)
	if err == nil {
		if existing.OrderItemID != command.OrderItemID || existing.WarehouseID != command.WarehouseID {
			return inventory.FulfillmentAllocation{}, inventory.ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return inventory.FulfillmentAllocation{}, err
	}
	var orderID, offerID, unit, orderStatus string
	var coefficient int64
	var scale int16
	if err := tx.QueryRowContext(ctx, `SELECT i.order_id,i.offer_id,i.quantity_coefficient,i.quantity_scale,i.unit,o.status FROM order_items i JOIN orders o ON o.organization_id=i.organization_id AND o.workspace_id=i.workspace_id AND o.id=i.order_id WHERE i.organization_id=$1 AND i.workspace_id=$2 AND i.id=$3`, s.OrganizationID(), s.WorkspaceID(), command.OrderItemID).Scan(&orderID, &offerID, &coefficient, &scale, &unit, &orderStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return inventory.FulfillmentAllocation{}, inventory.ErrNotFound
		}
		return inventory.FulfillmentAllocation{}, err
	}
	if orderStatus == "fulfilled" || orderStatus == "cancelled" {
		return inventory.FulfillmentAllocation{}, inventory.ErrInvalidRecord
	}
	decimal, err := inventory.NewDecimal(coefficient, uint8(scale))
	if err != nil {
		return inventory.FulfillmentAllocation{}, err
	}
	unitCode, err := inventory.NewUnitCode(unit)
	if err != nil {
		return inventory.FulfillmentAllocation{}, err
	}
	quantity, _ := inventory.NewQuantity(decimal, unitCode)
	position, err := positionByParentsForUpdate(ctx, tx, s, inventory.OfferID(offerID), command.WarehouseID)
	if err != nil {
		return inventory.FulfillmentAllocation{}, err
	}
	if position.OnHand.Unit != quantity.Unit {
		return inventory.FulfillmentAllocation{}, inventory.ErrInvalidRecord
	}
	if err := requireWarehouseAllocatable(ctx, tx, s, command.WarehouseID); err != nil {
		return inventory.FulfillmentAllocation{}, err
	}
	available, err := position.Available()
	if err != nil {
		return inventory.FulfillmentAllocation{}, err
	}
	cmp, err := available.Value.Cmp(quantity.Value)
	if err != nil || cmp < 0 {
		return inventory.FulfillmentAllocation{}, inventory.ErrInsufficientAvailable
	}
	reserved, err := position.Reserved.Value.Add(quantity.Value)
	if err != nil {
		return inventory.FulfillmentAllocation{}, err
	}
	if _, err := updateReservedTx(ctx, tx, s, position, reserved, mutation, "reserved", "order_fulfillment"); err != nil {
		return inventory.FulfillmentAllocation{}, err
	}
	var out inventory.FulfillmentAllocation
	now := mutation.OccurredAt.UTC()
	if err := scanAllocation(tx.QueryRowContext(ctx, `INSERT INTO fulfillment_allocations(organization_id,workspace_id,allocation_id,idempotency_key,order_id,order_item_id,offer_id,warehouse_id,quantity_coefficient,quantity_scale,unit,state,reason_code,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'reserved','order_fulfillment',1,$12,$12) RETURNING allocation_id,idempotency_key,order_id,order_item_id,offer_id,warehouse_id,quantity_coefficient,quantity_scale,unit,state,COALESCE(reason_code,''),COALESCE(incident_id,''),COALESCE(replaces_allocation_id,''),version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), command.AllocationID, command.IdempotencyKey, orderID, command.OrderItemID, offerID, command.WarehouseID.String(), coefficient, scale, unit, now), &out); err != nil {
		return inventory.FulfillmentAllocation{}, err
	}
	if err := enqueueAllocationEvent(ctx, tx, s, derivedMutation(mutation, "allocation"), out, "order_fulfillment"); err != nil {
		return inventory.FulfillmentAllocation{}, err
	}
	return out, nil
}

func derivedWMSMutation(base inventory.Mutation, itemID, suffix string) inventory.Mutation {
	base.EventID = wmsDerivedUUID(base.CorrelationID, itemID, suffix)
	base.AuditID = wmsDerivedUUID(base.CorrelationID, itemID, suffix+"_audit")
	return base
}

func wmsDerivedKey(prefix, key, itemID string) string {
	hash := sha256.Sum256([]byte(prefix + "\x00" + key + "\x00" + itemID))
	return prefix + "_" + hex.EncodeToString(hash[:16])
}

func wmsDerivedUUID(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte{0})
		hash.Write([]byte(part))
	}
	value := hash.Sum(nil)[:16]
	value[6] = (value[6] & 0x0f) | 0x70
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

func validTokenValue(value string) bool {
	return value != "" && len(value) <= 192 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, " \t\r\n")
}

func validReasonValue(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for index, char := range []byte(value) {
		if index == 0 {
			if char < 'a' || char > 'z' {
				return false
			}
			continue
		}
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func timePtr(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func mustInventoryQuantity(coefficient int64, unit inventory.UnitCode) inventory.Quantity {
	decimal, _ := inventory.NewDecimal(coefficient, 0)
	quantity, _ := inventory.NewQuantity(decimal, unit)
	return quantity
}
