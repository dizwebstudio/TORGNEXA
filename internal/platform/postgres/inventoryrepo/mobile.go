package inventoryrepo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/inventory"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
)

// MobilePlan is a tenant-scoped projection of a canonical fulfillment plan.
// It contains references to WMS/order facts and never becomes a second ledger.
type MobilePlan struct {
	ID, IdempotencyKey, OrderID, WarehouseID string
	Mode                                     inventory.FulfillmentMode
	Owner                                    inventory.FulfillmentOwner
	Stage, State                             string
	LocalExecution                           bool
	RemoteReferenceDigest                    string
	Version                                  int64
	CreatedAt, UpdatedAt                     time.Time
}

// CreateMobilePlan creates one provider-neutral FBS/FBO/hybrid projection.
type CreateMobilePlan struct {
	ID, IdempotencyKey, OrderID, WarehouseID string
	Mode                                     inventory.FulfillmentMode
	Owner                                    inventory.FulfillmentOwner
	LocalExecution                           bool
	RemoteReferenceDigest                    string
}

// AdvanceMobilePlanCommand moves a plan through a provider-neutral local or
// remote stage. It never performs an external write by itself.
type AdvanceMobilePlanCommand struct {
	PlanID, Stage, State, RemoteReferenceDigest string
}

// MobilePickBatch groups existing WMS pick tasks for a mobile route.
type MobilePickBatch struct {
	ID, IdempotencyKey, PlanID, WarehouseID string
	Strategy                                string
	State                                   string
	RouteDigest                             string
	TaskIDs                                 []string
	Version                                 int64
	CreatedAt, UpdatedAt                    time.Time
}

// CreateMobilePickBatch never creates reservations. The task IDs must already
// belong to canonical WMS tasks created by WMSCreateOrderPickTasks.
type CreateMobilePickBatch struct {
	ID, IdempotencyKey, PlanID, WarehouseID string
	Strategy                                string
	RouteDigest                             string
	TaskIDs                                 []string
}

// MobileScanEvidence is immutable mobile scan evidence. CodeDigest is the
// only representation of the scanned value retained by the repository.
type MobileScanEvidence struct {
	ID, IdempotencyKey, PlanID, TaskID, DeviceID string
	Kind                                         inventory.MobileScanKind
	CodeDigest, LocationCode, Result             string
	Quantity                                     inventory.Quantity
	OccurredAt                                   time.Time
}

// CreateMobilePack stores exact package facts captured before handoff.
type CreateMobilePack struct {
	ID, IdempotencyKey, PlanID, BatchID string
	Facts                               inventory.PackageFacts
	PackagingType, FactsDigest          string
}

// MobilePackSession is a durable packing station session.
type MobilePackSession struct {
	ID, IdempotencyKey, PlanID, BatchID, PackagingType string
	Facts                                              inventory.PackageFacts
	State, FactsDigest                                 string
	Version                                            int64
	CreatedAt, UpdatedAt                               time.Time
}

// QueueMobilePrint creates a host-owned print intent. It never claims that a
// physical printer accepted the document.
type QueueMobilePrint struct {
	ID, IdempotencyKey, PlanID, PackID, PrinterID string
	Document                                      inventory.MobilePrintDocument
	TemplateVersion, MediaSize, Language          string
	Copies                                        int
	Reprint                                       bool
	ReasonCode, ArtifactDigest                    string
}

// MobilePrintJob is the redacted state of a print intent.
type MobilePrintJob struct {
	ID, IdempotencyKey, PlanID, PackID, PrinterID string
	Document                                      inventory.MobilePrintDocument
	TemplateVersion, MediaSize, Language          string
	Copies                                        int
	State                                         string
	Reprint                                       bool
	ReasonCode, ArtifactDigest, ErrorCode         string
	Version                                       int64
	CreatedAt, UpdatedAt                          time.Time
}

// RegisterMobileDevice registers a warehouse-scoped handheld or station.
type RegisterMobileDevice struct {
	ID, IdempotencyKey, Label, WarehouseID, ZoneCode, OperatorID string
	Kind                                                         string
}

// MobileDevice is a redacted device registration and session boundary.
type MobileDevice struct {
	ID, IdempotencyKey, Label, WarehouseID, ZoneCode, Kind, State, OperatorID string
	LastSeenAt                                                                *time.Time
	Version                                                                   int64
	CreatedAt, UpdatedAt                                                      time.Time
}

// RecordMobileOfflineIntent stores only an encrypted-client-independent
// digest and the server receipt state. Raw command payloads are transient.
type RecordMobileOfflineIntent struct {
	ID, IdempotencyKey, DeviceID, PlanID string
	Operation                            inventory.MobileOperation
	PayloadDigest                        string
	SequenceNo                           int64
	State, ErrorCode                     string
}

// MobileOfflineIntent is a durable reconnect receipt.
type MobileOfflineIntent struct {
	ID, IdempotencyKey, DeviceID, PlanID string
	Operation                            inventory.MobileOperation
	PayloadDigest                        string
	SequenceNo                           int64
	State, ErrorCode                     string
	CreatedAt, UpdatedAt                 time.Time
}

// RecordMobileObservation records provider-authoritative FBO/handoff facts.
type RecordMobileObservation struct {
	ID, IdempotencyKey, PlanID, Stage, State string
	RemoteReferenceDigest                    string
	ObservedAt                               time.Time
}

// MobileSummary contains bounded counters for the handheld home screen.
type MobileSummary struct {
	ActivePlans, FBSPlans, FBOPlans, HybridPlans, SplitPlans    int64
	PendingTasks, ExceptionTasks, PendingPrints, OfflinePending int64
}

var (
	ErrMobilePlanMode      = errors.New("inventory repository: mobile plan does not allow local operation")
	ErrMobileDeviceRevoked = errors.New("inventory repository: mobile device is revoked")
	ErrMobilePrintState    = errors.New("inventory repository: invalid mobile print state")
	ErrMobilePlanState     = errors.New("inventory repository: invalid mobile plan state")
)

const mobilePlanSelect = `SELECT plan_id,idempotency_key,COALESCE(order_id,''),COALESCE(warehouse_id,''),mode,owner,stage,state,local_execution,remote_reference_digest,version,created_at,updated_at FROM mobile_fulfillment_plans WHERE organization_id=$1 AND workspace_id=$2`
const mobileDeviceSelect = `SELECT device_id,idempotency_key,label,warehouse_id,zone_code,device_kind,state,operator_id,last_seen_at,version,created_at,updated_at FROM mobile_devices WHERE organization_id=$1 AND workspace_id=$2`
const mobileBatchSelect = `SELECT batch_id,idempotency_key,plan_id,warehouse_id,strategy,state,route_digest,version,created_at,updated_at FROM mobile_pick_batches WHERE organization_id=$1 AND workspace_id=$2`
const mobilePackSelect = `SELECT pack_id,idempotency_key,plan_id,COALESCE(batch_id,''),package_count,weight_grams,length_mm,width_mm,height_mm,packaging_type,state,facts_digest,version,created_at,updated_at FROM mobile_pack_sessions WHERE organization_id=$1 AND workspace_id=$2`
const mobilePrintSelect = `SELECT print_job_id,idempotency_key,plan_id,COALESCE(pack_id,''),printer_id,document_type,template_version,media_size,language,copies,state,reprint,reason_code,artifact_digest,error_code,version,created_at,updated_at FROM mobile_print_jobs WHERE organization_id=$1 AND workspace_id=$2`
const mobileIntentSelect = `SELECT intent_id,idempotency_key,device_id,plan_id,operation,payload_digest,sequence_no,state,error_code,created_at,updated_at FROM mobile_offline_intents WHERE organization_id=$1 AND workspace_id=$2`

// CreateMobilePlan creates or replays a fulfillment plan. FBO plans are
// remote observations only and cannot carry local execution ownership.
func (r *Repository) CreateMobilePlan(ctx context.Context, s inventory.Scope, command CreateMobilePlan, mutation inventory.Mutation) (MobilePlan, bool, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(command.ID) || !validTokenValue(command.IdempotencyKey) || !validMobilePlanCommand(command) {
		return MobilePlan{}, false, inventory.ErrInvalidRecord
	}
	if command.RemoteReferenceDigest != "" && !validDigest(command.RemoteReferenceDigest) {
		return MobilePlan{}, false, inventory.ErrInvalidRecord
	}
	if command.LocalExecution {
		command.Owner = inventory.OwnerSellerWarehouse
	}
	if command.Mode == inventory.FulfillmentFBO {
		command.Owner = inventory.OwnerMarketplace
		command.LocalExecution = false
	}
	commandStage := "pick"
	if command.Mode == inventory.FulfillmentFBO {
		commandStage = "remote_visibility"
	}
	out := MobilePlan{}
	created := false
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var existingID string
		err := tx.QueryRowContext(ctx, `SELECT plan_id FROM mobile_fulfillment_plans WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), command.IdempotencyKey).Scan(&existingID)
		if err == nil {
			out, err = loadMobilePlan(ctx, tx, s, existingID, false)
			if err != nil {
				return err
			}
			if !sameMobilePlan(out, command) {
				return inventory.ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if command.LocalExecution {
			if err := requireWarehouseAllocatable(ctx, tx, s, inventory.WarehouseID(command.WarehouseID)); err != nil {
				return err
			}
		}
		out = MobilePlan{ID: command.ID, IdempotencyKey: command.IdempotencyKey, OrderID: command.OrderID, WarehouseID: command.WarehouseID, Mode: command.Mode, Owner: command.Owner, Stage: commandStage, State: "active", LocalExecution: command.LocalExecution, RemoteReferenceDigest: command.RemoteReferenceDigest, Version: 1, CreatedAt: mutation.OccurredAt.UTC(), UpdatedAt: mutation.OccurredAt.UTC()}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mobile_fulfillment_plans(organization_id,workspace_id,plan_id,idempotency_key,order_id,warehouse_id,mode,owner,stage,state,local_execution,remote_reference_digest,version,created_at,updated_at) VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,$9,'active',$10,$11,1,$12,$12)`, s.OrganizationID(), s.WorkspaceID(), out.ID, out.IdempotencyKey, out.OrderID, out.WarehouseID, out.Mode, out.Owner, out.Stage, out.LocalExecution, out.RemoteReferenceDigest, out.CreatedAt); err != nil {
			return err
		}
		created = true
		if err := appendAudit(ctx, tx, s, mutation, "mobile.fulfillment_plan.created", "mobile_fulfillment_plan", out.ID, audit.RiskWriteSafe, map[string]any{"plan_id": out.ID, "mode": out.Mode, "owner": out.Owner, "stage": out.Stage, "local_execution": out.LocalExecution, "version": out.Version}); err != nil {
			return err
		}
		return enqueueMobilePlanEvent(ctx, tx, s, mutation, out, "created")
	})
	return out, created, err
}

// ListMobilePlans returns the mobile home feed with cursor-free bounded
// filters; the mobile surface refreshes frequently and remains capped.
func (r *Repository) ListMobilePlans(ctx context.Context, s inventory.Scope, mode, state, warehouseID string, limit int) ([]MobilePlan, error) {
	if err := validate(ctx, r, s); err != nil || limit < 1 || limit > 200 || (mode != "" && !inventory.MobileFulfillmentModeValid(inventory.FulfillmentMode(mode))) || (state != "" && !validMobilePlanState(state)) || (warehouseID != "" && !validSortableIDValue(warehouseID)) {
		return nil, inventory.ErrInvalidRecord
	}
	items := make([]MobilePlan, 0, limit)
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		args := []any{s.OrganizationID(), s.WorkspaceID()}
		where := ""
		if mode != "" {
			args = append(args, mode)
			where += fmt.Sprintf(" AND mode=$%d", len(args))
		}
		if state != "" {
			args = append(args, state)
			where += fmt.Sprintf(" AND state=$%d", len(args))
		}
		if warehouseID != "" {
			args = append(args, warehouseID)
			where += fmt.Sprintf(" AND warehouse_id=$%d", len(args))
		}
		args = append(args, limit)
		rows, err := tx.QueryContext(ctx, mobilePlanSelect+where+fmt.Sprintf(" ORDER BY updated_at DESC,plan_id DESC LIMIT $%d", len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanMobilePlan(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// MobilePlan returns one tenant-scoped mobile plan.
func (r *Repository) MobilePlan(ctx context.Context, s inventory.Scope, id string) (MobilePlan, error) {
	if err := validate(ctx, r, s); err != nil || !validSortableIDValue(id) {
		return MobilePlan{}, inventory.ErrInvalidRecord
	}
	var out MobilePlan
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		var err error
		out, err = loadMobilePlan(ctx, tx, s, id, false)
		return err
	})
	return out, err
}

// AdvanceMobilePlan records a durable stage transition. FBO plans can only
// move through remote visibility/tracking stages; seller-owned plans can use
// local pick/pack/print/handoff stages.
func (r *Repository) AdvanceMobilePlan(ctx context.Context, s inventory.Scope, command AdvanceMobilePlanCommand, expectedVersion int64, mutation inventory.Mutation) (MobilePlan, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(command.PlanID) || !validMobilePlanStage(command.Stage) || !validMobilePlanState(command.State) || expectedVersion < 1 || (command.RemoteReferenceDigest != "" && !validDigest(command.RemoteReferenceDigest)) {
		return MobilePlan{}, inventory.ErrInvalidRecord
	}
	var out MobilePlan
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		current, err := loadMobilePlan(ctx, tx, s, command.PlanID, true)
		if err != nil {
			return err
		}
		if current.Stage == command.Stage && current.State == command.State && (command.RemoteReferenceDigest == "" || current.RemoteReferenceDigest == command.RemoteReferenceDigest) {
			out = current
			return nil
		}
		if current.Version != expectedVersion {
			return inventory.ErrConflict
		}
		if current.Mode == inventory.FulfillmentFBO && command.Stage != "remote_visibility" && command.Stage != "tracking" && command.Stage != "complete" && command.Stage != "manual_attention" {
			return ErrMobilePlanMode
		}
		if current.Mode != inventory.FulfillmentFBO && command.Stage == "remote_visibility" {
			return ErrMobilePlanMode
		}
		if command.Stage != "manual_attention" && current.Stage != "manual_attention" && mobilePlanStageRank(command.Stage) < mobilePlanStageRank(current.Stage) {
			return ErrMobilePlanState
		}
		if command.Stage == "complete" && command.State != "complete" || command.State == "complete" && command.Stage != "complete" {
			return ErrMobilePlanState
		}
		row := tx.QueryRowContext(ctx, `UPDATE mobile_fulfillment_plans SET stage=$4,state=$5,remote_reference_digest=CASE WHEN $6='' THEN remote_reference_digest ELSE $6 END,version=version+1,updated_at=$7 WHERE organization_id=$1 AND workspace_id=$2 AND plan_id=$3 AND version=$8 RETURNING plan_id,idempotency_key,COALESCE(order_id,''),COALESCE(warehouse_id,''),mode,owner,stage,state,local_execution,remote_reference_digest,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), command.PlanID, command.Stage, command.State, command.RemoteReferenceDigest, mutation.OccurredAt.UTC(), expectedVersion)
		out, err = scanMobilePlan(row)
		if errors.Is(err, inventory.ErrNotFound) {
			return inventory.ErrConflict
		}
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, s, mutation, "mobile.fulfillment_plan.advanced", "mobile_fulfillment_plan", out.ID, audit.RiskWriteSafe, map[string]any{"stage": out.Stage, "state": out.State, "version": out.Version, "remote_reference_digest": out.RemoteReferenceDigest}); err != nil {
			return err
		}
		return enqueueMobilePlanEvent(ctx, tx, s, mutation, out, "advanced")
	})
	return out, err
}

// CreateMobilePickBatch groups existing WMS tasks without touching stock or
// reservations. FBO plans are rejected before any write.
func (r *Repository) CreateMobilePickBatch(ctx context.Context, s inventory.Scope, command CreateMobilePickBatch, mutation inventory.Mutation) (MobilePickBatch, bool, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(command.ID) || !validTokenValue(command.IdempotencyKey) || !validSortableIDValue(command.PlanID) || !validSortableIDValue(command.WarehouseID) || !validMobileBatchStrategy(command.Strategy) || !validMobileTaskIDs(command.TaskIDs) || (command.RouteDigest != "" && !validDigest(command.RouteDigest)) {
		return MobilePickBatch{}, false, inventory.ErrInvalidRecord
	}
	var out MobilePickBatch
	created := false
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		plan, err := loadMobilePlan(ctx, tx, s, command.PlanID, true)
		if err != nil {
			return err
		}
		if !plan.LocalExecution || !inventory.MobileLocalOperationAllowed(plan.Mode, inventory.MobileOperationPick) || plan.WarehouseID != command.WarehouseID {
			return ErrMobilePlanMode
		}
		var existingID string
		err = tx.QueryRowContext(ctx, `SELECT batch_id FROM mobile_pick_batches WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), command.IdempotencyKey).Scan(&existingID)
		if err == nil {
			out, err = loadMobilePickBatch(ctx, tx, s, existingID, false)
			if err != nil {
				return err
			}
			if !sameMobilePickBatch(out, command) {
				return inventory.ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		placeholders := make([]string, len(command.TaskIDs))
		args := []any{s.OrganizationID(), s.WorkspaceID()}
		for i, taskID := range command.TaskIDs {
			placeholders[i] = fmt.Sprintf("$%d", len(args)+1)
			args = append(args, taskID)
		}
		rows, err := tx.QueryContext(ctx, `SELECT task_id,task_type,state,warehouse_id FROM wms_execution_tasks WHERE organization_id=$1 AND workspace_id=$2 AND task_id IN (`+strings.Join(placeholders, ",")+") FOR UPDATE", args...)
		if err != nil {
			return err
		}
		type taskRef struct{ ID, Type, State, Warehouse string }
		found := make(map[string]taskRef, len(command.TaskIDs))
		for rows.Next() {
			var item taskRef
			if err := rows.Scan(&item.ID, &item.Type, &item.State, &item.Warehouse); err != nil {
				_ = rows.Close()
				return err
			}
			found[item.ID] = item
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, taskID := range command.TaskIDs {
			item, ok := found[taskID]
			if !ok {
				return inventory.ErrNotFound
			}
			if item.Type != "pick" || item.Warehouse != command.WarehouseID || item.State == "cancelled" || item.State == "exception" {
				return inventory.ErrConflict
			}
			var duplicate string
			if err := tx.QueryRowContext(ctx, `SELECT batch_id FROM mobile_pick_batch_tasks WHERE organization_id=$1 AND workspace_id=$2 AND task_id=$3`, s.OrganizationID(), s.WorkspaceID(), taskID).Scan(&duplicate); err == nil {
				return inventory.ErrConflict
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		out = MobilePickBatch{ID: command.ID, IdempotencyKey: command.IdempotencyKey, PlanID: command.PlanID, WarehouseID: command.WarehouseID, Strategy: command.Strategy, State: "ready", RouteDigest: command.RouteDigest, TaskIDs: append([]string(nil), command.TaskIDs...), Version: 1, CreatedAt: mutation.OccurredAt.UTC(), UpdatedAt: mutation.OccurredAt.UTC()}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mobile_pick_batches(organization_id,workspace_id,batch_id,idempotency_key,plan_id,warehouse_id,strategy,state,route_digest,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,'ready',$8,1,$9,$9)`, s.OrganizationID(), s.WorkspaceID(), out.ID, out.IdempotencyKey, out.PlanID, out.WarehouseID, out.Strategy, out.RouteDigest, out.CreatedAt); err != nil {
			return err
		}
		for i, taskID := range command.TaskIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO mobile_pick_batch_tasks(organization_id,workspace_id,batch_id,task_id,position) VALUES($1,$2,$3,$4,$5)`, s.OrganizationID(), s.WorkspaceID(), out.ID, taskID, i+1); err != nil {
				return err
			}
		}
		created = true
		if err := appendAudit(ctx, tx, s, mutation, "mobile.pick_batch.created", "mobile_pick_batch", out.ID, audit.RiskWriteSafe, map[string]any{"plan_id": out.PlanID, "warehouse_id": out.WarehouseID, "strategy": out.Strategy, "task_count": len(out.TaskIDs), "route_digest": out.RouteDigest}); err != nil {
			return err
		}
		return enqueueMobileBatchEvent(ctx, tx, s, mutation, out, "created")
	})
	return out, created, err
}

// ListMobilePickBatches returns recent tenant-scoped pick routes.
func (r *Repository) ListMobilePickBatches(ctx context.Context, s inventory.Scope, planID string, limit int) ([]MobilePickBatch, error) {
	if err := validate(ctx, r, s); err != nil || limit < 1 || limit > 100 || (planID != "" && !validSortableIDValue(planID)) {
		return nil, inventory.ErrInvalidRecord
	}
	items := make([]MobilePickBatch, 0, limit)
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		args := []any{s.OrganizationID(), s.WorkspaceID()}
		where := ""
		if planID != "" {
			args = append(args, planID)
			where = " AND plan_id=$3"
		}
		args = append(args, limit)
		rows, err := tx.QueryContext(ctx, mobileBatchSelect+where+fmt.Sprintf(" ORDER BY updated_at DESC,batch_id DESC LIMIT $%d", len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanMobilePickBatch(rows)
			if err != nil {
				return err
			}
			item.TaskIDs, err = loadMobileBatchTaskIDs(ctx, tx, s, item.ID)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// ListMobilePacks returns recent packing sessions for the warehouse workspace.
func (r *Repository) ListMobilePacks(ctx context.Context, s inventory.Scope, planID string, limit int) ([]MobilePackSession, error) {
	if err := validate(ctx, r, s); err != nil || limit < 1 || limit > 100 || (planID != "" && !validSortableIDValue(planID)) {
		return nil, inventory.ErrInvalidRecord
	}
	items := make([]MobilePackSession, 0, limit)
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		args := []any{s.OrganizationID(), s.WorkspaceID()}
		where := ""
		if planID != "" {
			args = append(args, planID)
			where = " AND plan_id=$3"
		}
		args = append(args, limit)
		rows, err := tx.QueryContext(ctx, mobilePackSelect+where+fmt.Sprintf(" ORDER BY updated_at DESC,pack_id DESC LIMIT $%d", len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanMobilePack(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// RecordMobileScan records metadata after the canonical WMS scan has been
// applied. The API intentionally calls WMSScanTask first so progress cannot
// be advanced by the mobile projection alone.
func (r *Repository) RecordMobileScan(ctx context.Context, s inventory.Scope, evidence MobileScanEvidence, mutation inventory.Mutation) (MobileScanEvidence, bool, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(evidence.ID) || !validTokenValue(evidence.IdempotencyKey) || !validSortableIDValue(evidence.PlanID) || !validSortableIDValue(evidence.TaskID) || !validSortableIDValue(evidence.DeviceID) || evidence.Kind == "" || !validDigest(evidence.CodeDigest) || inventory.ValidateMobileLocation(evidence.LocationCode) != nil || evidence.Quantity.Validate() != nil || evidence.Quantity.Value.IsZero() || !isMobileResult(evidence.Result) {
		return MobileScanEvidence{}, false, inventory.ErrInvalidRecord
	}
	var out MobileScanEvidence
	created := false
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		plan, err := loadMobilePlan(ctx, tx, s, evidence.PlanID, true)
		if err != nil {
			return err
		}
		if !plan.LocalExecution || !inventory.MobileLocalOperationAllowed(plan.Mode, inventory.MobileOperationScan) {
			return ErrMobilePlanMode
		}
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM mobile_devices WHERE organization_id=$1 AND workspace_id=$2 AND device_id=$3`, s.OrganizationID(), s.WorkspaceID(), evidence.DeviceID).Scan(&state); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return inventory.ErrNotFound
			}
			return err
		}
		if state != "active" {
			return ErrMobileDeviceRevoked
		}
		var existingID string
		err = tx.QueryRowContext(ctx, `SELECT scan_id FROM mobile_scan_evidence WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), evidence.IdempotencyKey).Scan(&existingID)
		if err == nil {
			out, err = loadMobileScan(ctx, tx, s, existingID)
			if err == nil && (out.PlanID != evidence.PlanID || out.TaskID != evidence.TaskID || out.DeviceID != evidence.DeviceID || out.Kind != evidence.Kind || out.CodeDigest != evidence.CodeDigest || out.LocationCode != evidence.LocationCode || !sameWMSQuantity(out.Quantity, evidence.Quantity)) {
				return inventory.ErrConflict
			}
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		out = evidence
		out.OccurredAt = evidence.OccurredAt.UTC()
		if _, err := tx.ExecContext(ctx, `INSERT INTO mobile_scan_evidence(organization_id,workspace_id,scan_id,idempotency_key,plan_id,task_id,device_id,kind,code_digest,location_code,quantity_coefficient,quantity_scale,unit,result,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, s.OrganizationID(), s.WorkspaceID(), out.ID, out.IdempotencyKey, out.PlanID, out.TaskID, out.DeviceID, out.Kind, out.CodeDigest, out.LocationCode, out.Quantity.Value.Coefficient(), out.Quantity.Value.Scale(), out.Quantity.Unit.String(), out.Result, out.OccurredAt); err != nil {
			return err
		}
		created = true
		if _, err := tx.ExecContext(ctx, `UPDATE mobile_devices SET last_seen_at=$4,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND device_id=$3 AND state='active'`, s.OrganizationID(), s.WorkspaceID(), evidence.DeviceID, out.OccurredAt); err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, s, mutation, "mobile.scan.recorded", "mobile_scan_evidence", out.ID, audit.RiskWriteSafe, map[string]any{"plan_id": out.PlanID, "task_id": out.TaskID, "device_id": out.DeviceID, "kind": out.Kind, "code_digest": out.CodeDigest, "location_code": out.LocationCode, "quantity": out.Quantity.Value.String(), "result": out.Result}); err != nil {
			return err
		}
		return enqueueMobileScanEvent(ctx, tx, s, mutation, out)
	})
	return out, created, err
}

// CreateMobilePack starts a pack session and stores exact weight/dimensions.
func (r *Repository) CreateMobilePack(ctx context.Context, s inventory.Scope, command CreateMobilePack, mutation inventory.Mutation) (MobilePackSession, bool, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(command.ID) || !validTokenValue(command.IdempotencyKey) || !validSortableIDValue(command.PlanID) || (command.BatchID != "" && !validSortableIDValue(command.BatchID)) || command.Facts.Validate() != nil || !validDigest(command.FactsDigest) || len(command.PackagingType) > 128 {
		return MobilePackSession{}, false, inventory.ErrInvalidRecord
	}
	var out MobilePackSession
	created := false
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		plan, err := loadMobilePlan(ctx, tx, s, command.PlanID, true)
		if err != nil {
			return err
		}
		if !plan.LocalExecution || !inventory.MobileLocalOperationAllowed(plan.Mode, inventory.MobileOperationPack) {
			return ErrMobilePlanMode
		}
		if command.BatchID != "" {
			var batchPlan string
			if err := tx.QueryRowContext(ctx, `SELECT plan_id FROM mobile_pick_batches WHERE organization_id=$1 AND workspace_id=$2 AND batch_id=$3`, s.OrganizationID(), s.WorkspaceID(), command.BatchID).Scan(&batchPlan); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return inventory.ErrNotFound
				}
				return err
			}
			if batchPlan != command.PlanID {
				return inventory.ErrConflict
			}
			var incomplete int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mobile_pick_batch_tasks t JOIN wms_execution_tasks w ON w.organization_id=t.organization_id AND w.workspace_id=t.workspace_id AND w.task_id=t.task_id WHERE t.organization_id=$1 AND t.workspace_id=$2 AND t.batch_id=$3 AND w.state <> 'completed'`, s.OrganizationID(), s.WorkspaceID(), command.BatchID).Scan(&incomplete); err != nil {
				return err
			}
			if incomplete > 0 {
				return ErrMobilePlanState
			}
		}
		var existingID string
		err = tx.QueryRowContext(ctx, `SELECT pack_id FROM mobile_pack_sessions WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), command.IdempotencyKey).Scan(&existingID)
		if err == nil {
			out, err = loadMobilePack(ctx, tx, s, existingID)
			if err != nil {
				return err
			}
			if !sameMobilePack(out, command) {
				return inventory.ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		out = MobilePackSession{ID: command.ID, IdempotencyKey: command.IdempotencyKey, PlanID: command.PlanID, BatchID: command.BatchID, Facts: command.Facts, PackagingType: command.PackagingType, State: "open", FactsDigest: command.FactsDigest, Version: 1, CreatedAt: mutation.OccurredAt.UTC(), UpdatedAt: mutation.OccurredAt.UTC()}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mobile_pack_sessions(organization_id,workspace_id,pack_id,idempotency_key,plan_id,batch_id,package_count,weight_grams,length_mm,width_mm,height_mm,packaging_type,state,facts_digest,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,$12,'open',$13,1,$14,$14)`, s.OrganizationID(), s.WorkspaceID(), out.ID, out.IdempotencyKey, out.PlanID, out.BatchID, out.Facts.PackageCount, out.Facts.WeightGrams, out.Facts.LengthMM, out.Facts.WidthMM, out.Facts.HeightMM, out.PackagingType, out.FactsDigest, out.CreatedAt); err != nil {
			return err
		}
		created = true
		if err := appendAudit(ctx, tx, s, mutation, "mobile.pack.created", "mobile_pack_session", out.ID, audit.RiskWriteSafe, map[string]any{"plan_id": out.PlanID, "batch_id": out.BatchID, "package_count": out.Facts.PackageCount, "weight_grams": out.Facts.WeightGrams, "length_mm": out.Facts.LengthMM, "width_mm": out.Facts.WidthMM, "height_mm": out.Facts.HeightMM, "facts_digest": out.FactsDigest}); err != nil {
			return err
		}
		return enqueueMobilePackEvent(ctx, tx, s, mutation, out, "created")
	})
	return out, created, err
}

// CloseMobilePack seals a pack session after its exact facts are confirmed.
func (r *Repository) CloseMobilePack(ctx context.Context, s inventory.Scope, id string, expectedVersion int64, mutation inventory.Mutation) (MobilePackSession, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(id) || expectedVersion < 1 {
		return MobilePackSession{}, inventory.ErrInvalidRecord
	}
	var out MobilePackSession
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		current, err := loadMobilePack(ctx, tx, s, id)
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return inventory.ErrConflict
		}
		if current.State != "open" {
			if current.State == "closed" {
				out = current
				return nil
			}
			return ErrMobilePlanState
		}
		row := tx.QueryRowContext(ctx, `UPDATE mobile_pack_sessions SET state='closed',version=version+1,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND pack_id=$3 AND version=$5 RETURNING pack_id,idempotency_key,plan_id,COALESCE(batch_id,''),package_count,weight_grams,length_mm,width_mm,height_mm,packaging_type,state,facts_digest,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), id, mutation.OccurredAt.UTC(), expectedVersion)
		out, err = scanMobilePack(row)
		if errors.Is(err, inventory.ErrNotFound) {
			return inventory.ErrConflict
		}
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, s, mutation, "mobile.pack.closed", "mobile_pack_session", out.ID, audit.RiskWriteSafe, map[string]any{"plan_id": out.PlanID, "facts_digest": out.FactsDigest, "version": out.Version}); err != nil {
			return err
		}
		return enqueueMobilePackEvent(ctx, tx, s, mutation, out, "closed")
	})
	return out, err
}

// QueueMobilePrint adds one idempotent host-owned print intent. FBO plans are
// intentionally excluded because remote documents are only observations.
func (r *Repository) QueueMobilePrint(ctx context.Context, s inventory.Scope, command QueueMobilePrint, mutation inventory.Mutation) (MobilePrintJob, bool, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(command.ID) || !validTokenValue(command.IdempotencyKey) || !validSortableIDValue(command.PlanID) || (command.PackID != "" && !validSortableIDValue(command.PackID)) || !validSortableIDValue(command.PrinterID) || inventory.ValidateMobilePrintRequest(command.Document, command.Copies) != nil || command.TemplateVersion == "" || len(command.TemplateVersion) > 64 || len(command.MediaSize) > 32 || len(command.Language) < 2 || len(command.Language) > 16 || len(command.ReasonCode) > 128 || (command.ArtifactDigest != "" && !validDigest(command.ArtifactDigest)) {
		return MobilePrintJob{}, false, inventory.ErrInvalidRecord
	}
	var out MobilePrintJob
	created := false
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		plan, err := loadMobilePlan(ctx, tx, s, command.PlanID, true)
		if err != nil {
			return err
		}
		if !plan.LocalExecution || !inventory.MobileLocalOperationAllowed(plan.Mode, inventory.MobileOperationPrint) {
			return ErrMobilePlanMode
		}
		if command.PackID != "" {
			var packPlan string
			if err := tx.QueryRowContext(ctx, `SELECT plan_id FROM mobile_pack_sessions WHERE organization_id=$1 AND workspace_id=$2 AND pack_id=$3`, s.OrganizationID(), s.WorkspaceID(), command.PackID).Scan(&packPlan); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return inventory.ErrNotFound
				}
				return err
			}
			if packPlan != command.PlanID {
				return inventory.ErrConflict
			}
		}
		var existingID string
		err = tx.QueryRowContext(ctx, `SELECT print_job_id FROM mobile_print_jobs WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), command.IdempotencyKey).Scan(&existingID)
		if err == nil {
			out, err = loadMobilePrint(ctx, tx, s, existingID)
			if err == nil && !sameMobilePrint(out, command) {
				return inventory.ErrConflict
			}
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		out = MobilePrintJob{ID: command.ID, IdempotencyKey: command.IdempotencyKey, PlanID: command.PlanID, PackID: command.PackID, PrinterID: command.PrinterID, Document: command.Document, TemplateVersion: command.TemplateVersion, MediaSize: command.MediaSize, Language: command.Language, Copies: command.Copies, State: "queued", Reprint: command.Reprint, ReasonCode: command.ReasonCode, ArtifactDigest: command.ArtifactDigest, Version: 1, CreatedAt: mutation.OccurredAt.UTC(), UpdatedAt: mutation.OccurredAt.UTC()}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mobile_print_jobs(organization_id,workspace_id,print_job_id,idempotency_key,plan_id,pack_id,printer_id,document_type,template_version,media_size,language,copies,state,reprint,reason_code,artifact_digest,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,$12,'queued',$13,$14,$15,1,$16,$16)`, s.OrganizationID(), s.WorkspaceID(), out.ID, out.IdempotencyKey, out.PlanID, out.PackID, out.PrinterID, out.Document, out.TemplateVersion, out.MediaSize, out.Language, out.Copies, out.Reprint, out.ReasonCode, out.ArtifactDigest, out.CreatedAt); err != nil {
			return err
		}
		created = true
		if err := appendAudit(ctx, tx, s, mutation, "mobile.print.queued", "mobile_print_job", out.ID, audit.RiskWriteSafe, map[string]any{"plan_id": out.PlanID, "pack_id": out.PackID, "printer_id": out.PrinterID, "document_type": out.Document, "copies": out.Copies, "reprint": out.Reprint, "reason_code": out.ReasonCode, "artifact_digest": out.ArtifactDigest}); err != nil {
			return err
		}
		return enqueueMobilePrintEvent(ctx, tx, s, mutation, out, "queued")
	})
	return out, created, err
}

// ListMobilePrintJobs returns redacted print queue state.
func (r *Repository) ListMobilePrintJobs(ctx context.Context, s inventory.Scope, planID string, limit int) ([]MobilePrintJob, error) {
	if err := validate(ctx, r, s); err != nil || limit < 1 || limit > 100 || (planID != "" && !validSortableIDValue(planID)) {
		return nil, inventory.ErrInvalidRecord
	}
	items := make([]MobilePrintJob, 0, limit)
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		args := []any{s.OrganizationID(), s.WorkspaceID()}
		where := ""
		if planID != "" {
			args = append(args, planID)
			where = " AND plan_id=$3"
		}
		args = append(args, limit)
		rows, err := tx.QueryContext(ctx, mobilePrintSelect+where+fmt.Sprintf(" ORDER BY updated_at DESC,print_job_id DESC LIMIT $%d", len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanMobilePrint(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// RecordMobilePrintStatus records a printer/edge receipt without silently
// retrying an unknown physical effect.
func (r *Repository) RecordMobilePrintStatus(ctx context.Context, s inventory.Scope, id, state, errorCode string, expectedVersion int64, mutation inventory.Mutation) (MobilePrintJob, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(id) || !validMobilePrintState(state) || len(errorCode) > 128 || expectedVersion < 1 {
		return MobilePrintJob{}, inventory.ErrInvalidRecord
	}
	var out MobilePrintJob
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		current, err := loadMobilePrint(ctx, tx, s, id)
		if err != nil {
			return err
		}
		if current.State != "queued" && current.State != "unknown" {
			if current.State == state && current.ErrorCode == errorCode {
				out = current
				return nil
			}
			return ErrMobilePrintState
		}
		if current.Version != expectedVersion {
			return inventory.ErrConflict
		}
		if state == "printed" && current.Reprint && current.ReasonCode == "" {
			return inventory.ErrInvalidRecord
		}
		row := tx.QueryRowContext(ctx, `UPDATE mobile_print_jobs SET state=$4,error_code=$5,version=version+1,updated_at=$6 WHERE organization_id=$1 AND workspace_id=$2 AND print_job_id=$3 AND version=$7 RETURNING print_job_id,idempotency_key,plan_id,COALESCE(pack_id,''),printer_id,document_type,template_version,media_size,language,copies,state,reprint,reason_code,artifact_digest,error_code,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), id, state, errorCode, mutation.OccurredAt.UTC(), expectedVersion)
		out, err = scanMobilePrint(row)
		if errors.Is(err, inventory.ErrNotFound) {
			return inventory.ErrConflict
		}
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, s, mutation, "mobile.print."+state, "mobile_print_job", out.ID, audit.RiskWriteSafe, map[string]any{"plan_id": out.PlanID, "printer_id": out.PrinterID, "state": out.State, "error_code": out.ErrorCode, "version": out.Version}); err != nil {
			return err
		}
		return enqueueMobilePrintEvent(ctx, tx, s, mutation, out, state)
	})
	return out, err
}

// RegisterMobileDevice creates or replays a warehouse-scoped device record.
func (r *Repository) RegisterMobileDevice(ctx context.Context, s inventory.Scope, command RegisterMobileDevice, mutation inventory.Mutation) (MobileDevice, bool, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(command.ID) || !validTokenValue(command.IdempotencyKey) || !validSortableIDValue(command.WarehouseID) || !validSortableIDValue(command.OperatorID) || !validMobileDeviceKind(command.Kind) || strings.TrimSpace(command.Label) != command.Label || command.Label == "" || len(command.Label) > 160 || inventory.ValidateMobileLocation(command.ZoneCode) != nil {
		return MobileDevice{}, false, inventory.ErrInvalidRecord
	}
	var out MobileDevice
	created := false
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		if err := requireWarehouseAllocatable(ctx, tx, s, inventory.WarehouseID(command.WarehouseID)); err != nil {
			return err
		}
		var existingID string
		err := tx.QueryRowContext(ctx, `SELECT device_id FROM mobile_devices WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), command.IdempotencyKey).Scan(&existingID)
		if err == nil {
			out, err = loadMobileDevice(ctx, tx, s, existingID)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		out = MobileDevice{ID: command.ID, IdempotencyKey: command.IdempotencyKey, Label: command.Label, WarehouseID: command.WarehouseID, ZoneCode: command.ZoneCode, Kind: command.Kind, State: "active", OperatorID: command.OperatorID, Version: 1, CreatedAt: mutation.OccurredAt.UTC(), UpdatedAt: mutation.OccurredAt.UTC()}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mobile_devices(organization_id,workspace_id,device_id,idempotency_key,label,warehouse_id,zone_code,device_kind,state,operator_id,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'active',$9,1,$10,$10)`, s.OrganizationID(), s.WorkspaceID(), out.ID, out.IdempotencyKey, out.Label, out.WarehouseID, out.ZoneCode, out.Kind, out.OperatorID, out.CreatedAt); err != nil {
			return err
		}
		created = true
		return appendAudit(ctx, tx, s, mutation, "mobile.device.registered", "mobile_device", out.ID, audit.RiskWriteSafe, map[string]any{"device_id": out.ID, "warehouse_id": out.WarehouseID, "zone_code": out.ZoneCode, "device_kind": out.Kind, "operator_id": out.OperatorID})
	})
	return out, created, err
}

// ListMobileDevices returns device registrations without session credentials.
func (r *Repository) ListMobileDevices(ctx context.Context, s inventory.Scope, warehouseID string, limit int) ([]MobileDevice, error) {
	if err := validate(ctx, r, s); err != nil || limit < 1 || limit > 100 || (warehouseID != "" && !validSortableIDValue(warehouseID)) {
		return nil, inventory.ErrInvalidRecord
	}
	items := make([]MobileDevice, 0, limit)
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		args := []any{s.OrganizationID(), s.WorkspaceID()}
		where := ""
		if warehouseID != "" {
			args = append(args, warehouseID)
			where = " AND warehouse_id=$3"
		}
		args = append(args, limit)
		rows, err := tx.QueryContext(ctx, mobileDeviceSelect+where+fmt.Sprintf(" ORDER BY updated_at DESC,device_id DESC LIMIT $%d", len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanMobileDevice(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// RevokeMobileDevice fences a lost or compromised handheld immediately.
func (r *Repository) RevokeMobileDevice(ctx context.Context, s inventory.Scope, id string, expectedVersion int64, mutation inventory.Mutation) (MobileDevice, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(id) || expectedVersion < 1 {
		return MobileDevice{}, inventory.ErrInvalidRecord
	}
	var out MobileDevice
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `UPDATE mobile_devices SET state='revoked',version=version+1,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND device_id=$3 AND version=$5 RETURNING device_id,idempotency_key,label,warehouse_id,zone_code,device_kind,state,operator_id,last_seen_at,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), id, mutation.OccurredAt.UTC(), expectedVersion)
		var err error
		out, err = scanMobileDevice(row)
		if errors.Is(err, inventory.ErrNotFound) {
			return inventory.ErrConflict
		}
		if err != nil {
			return err
		}
		return appendAudit(ctx, tx, s, mutation, "mobile.device.revoked", "mobile_device", out.ID, audit.RiskWriteSensitive, map[string]any{"device_id": out.ID, "version": out.Version})
	})
	return out, err
}

// RecordMobileOfflineIntent records a reconnect receipt without payload data.
func (r *Repository) RecordMobileOfflineIntent(ctx context.Context, s inventory.Scope, command RecordMobileOfflineIntent, mutation inventory.Mutation) (MobileOfflineIntent, bool, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(command.ID) || !validTokenValue(command.IdempotencyKey) || !validSortableIDValue(command.DeviceID) || !validSortableIDValue(command.PlanID) || !validDigest(command.PayloadDigest) || command.SequenceNo < 0 || !validMobileOperation(command.Operation) || !validMobileIntentState(command.State) || len(command.ErrorCode) > 128 {
		return MobileOfflineIntent{}, false, inventory.ErrInvalidRecord
	}
	var out MobileOfflineIntent
	created := false
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var deviceState string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM mobile_devices WHERE organization_id=$1 AND workspace_id=$2 AND device_id=$3`, s.OrganizationID(), s.WorkspaceID(), command.DeviceID).Scan(&deviceState); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return inventory.ErrNotFound
			}
			return err
		}
		if deviceState != "active" {
			return ErrMobileDeviceRevoked
		}
		var existingID string
		err := tx.QueryRowContext(ctx, `SELECT intent_id FROM mobile_offline_intents WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), command.IdempotencyKey).Scan(&existingID)
		if err == nil {
			out, err = loadMobileIntent(ctx, tx, s, existingID)
			if err == nil && (out.DeviceID != command.DeviceID || out.PlanID != command.PlanID || out.Operation != command.Operation || out.PayloadDigest != command.PayloadDigest || out.SequenceNo != command.SequenceNo) {
				return inventory.ErrConflict
			}
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		out = MobileOfflineIntent{ID: command.ID, IdempotencyKey: command.IdempotencyKey, DeviceID: command.DeviceID, PlanID: command.PlanID, Operation: command.Operation, PayloadDigest: command.PayloadDigest, SequenceNo: command.SequenceNo, State: command.State, ErrorCode: command.ErrorCode, CreatedAt: mutation.OccurredAt.UTC(), UpdatedAt: mutation.OccurredAt.UTC()}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mobile_offline_intents(organization_id,workspace_id,intent_id,idempotency_key,device_id,plan_id,operation,payload_digest,sequence_no,state,error_code,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)`, s.OrganizationID(), s.WorkspaceID(), out.ID, out.IdempotencyKey, out.DeviceID, out.PlanID, out.Operation, out.PayloadDigest, out.SequenceNo, out.State, out.ErrorCode, out.CreatedAt); err != nil {
			return err
		}
		created = true
		return appendAudit(ctx, tx, s, mutation, "mobile.offline_intent."+out.State, "mobile_offline_intent", out.ID, audit.RiskWriteSafe, map[string]any{"intent_id": out.ID, "plan_id": out.PlanID, "device_id": out.DeviceID, "operation": out.Operation, "payload_digest": out.PayloadDigest, "sequence_no": out.SequenceNo, "state": out.State, "error_code": out.ErrorCode})
	})
	return out, created, err
}

// ListMobileOfflineIntents returns the bounded reconnect queue.
func (r *Repository) ListMobileOfflineIntents(ctx context.Context, s inventory.Scope, deviceID, state string, limit int) ([]MobileOfflineIntent, error) {
	if err := validate(ctx, r, s); err != nil || limit < 1 || limit > 200 || (deviceID != "" && !validSortableIDValue(deviceID)) || (state != "" && !validMobileIntentState(state)) {
		return nil, inventory.ErrInvalidRecord
	}
	items := make([]MobileOfflineIntent, 0, limit)
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		args := []any{s.OrganizationID(), s.WorkspaceID()}
		where := ""
		if deviceID != "" {
			args = append(args, deviceID)
			where += fmt.Sprintf(" AND device_id=$%d", len(args))
		}
		if state != "" {
			args = append(args, state)
			where += fmt.Sprintf(" AND state=$%d", len(args))
		}
		args = append(args, limit)
		rows, err := tx.QueryContext(ctx, mobileIntentSelect+where+fmt.Sprintf(" ORDER BY sequence_no,intent_id LIMIT $%d", len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanMobileIntent(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// RecordMobileObservation stores provider-authoritative status and advances the
// plan projection only through an optimistic version update.
func (r *Repository) RecordMobileObservation(ctx context.Context, s inventory.Scope, command RecordMobileObservation, expectedVersion int64, mutation inventory.Mutation) (MobilePlan, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil || !validSortableIDValue(command.ID) || !validTokenValue(command.IdempotencyKey) || !validSortableIDValue(command.PlanID) || !validMobileObservationStage(command.Stage) || !validMobileObservationState(command.State) || (command.RemoteReferenceDigest != "" && !validDigest(command.RemoteReferenceDigest)) || expectedVersion < 1 || command.ObservedAt.IsZero() || command.ObservedAt.Location() != time.UTC {
		return MobilePlan{}, inventory.ErrInvalidRecord
	}
	var out MobilePlan
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		plan, err := loadMobilePlan(ctx, tx, s, command.PlanID, true)
		if err != nil {
			return err
		}
		var existing string
		if err := tx.QueryRowContext(ctx, `SELECT observation_id FROM mobile_remote_observations WHERE organization_id=$1 AND workspace_id=$2 AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), command.IdempotencyKey).Scan(&existing); err == nil {
			out = plan
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if plan.Version != expectedVersion {
			return inventory.ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mobile_remote_observations(organization_id,workspace_id,observation_id,idempotency_key,plan_id,stage,state,remote_reference_digest,observed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, s.OrganizationID(), s.WorkspaceID(), command.ID, command.IdempotencyKey, command.PlanID, command.Stage, command.State, command.RemoteReferenceDigest, command.ObservedAt.UTC()); err != nil {
			return err
		}
		planState := command.State
		if command.State == "observed" || command.State == "accepted" {
			planState = "active"
		}
		if command.Stage == "complete" {
			planState = "complete"
		}
		row := tx.QueryRowContext(ctx, `UPDATE mobile_fulfillment_plans SET stage=$4,state=$5,remote_reference_digest=$6,version=version+1,updated_at=$7 WHERE organization_id=$1 AND workspace_id=$2 AND plan_id=$3 AND version=$8 RETURNING plan_id,idempotency_key,COALESCE(order_id,''),COALESCE(warehouse_id,''),mode,owner,stage,state,local_execution,remote_reference_digest,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), command.PlanID, command.Stage, planState, command.RemoteReferenceDigest, mutation.OccurredAt.UTC(), expectedVersion)
		out, err = scanMobilePlan(row)
		if errors.Is(err, inventory.ErrNotFound) {
			return inventory.ErrConflict
		}
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, s, mutation, "mobile.remote_observation.recorded", "mobile_remote_observation", command.ID, audit.RiskWriteSafe, map[string]any{"plan_id": command.PlanID, "stage": command.Stage, "state": command.State, "remote_reference_digest": command.RemoteReferenceDigest, "version": out.Version}); err != nil {
			return err
		}
		return enqueueMobilePlanEvent(ctx, tx, s, mutation, out, "remote_observed")
	})
	return out, err
}

// MobileSummary returns bounded counters for the mobile home screen.
func (r *Repository) MobileSummary(ctx context.Context, s inventory.Scope) (MobileSummary, error) {
	if err := validate(ctx, r, s); err != nil {
		return MobileSummary{}, err
	}
	var out MobileSummary
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE state IN ('active','pending','unknown','manual_attention')),COUNT(*) FILTER (WHERE mode='fbs' AND state IN ('active','pending','unknown','manual_attention')),COUNT(*) FILTER (WHERE mode='fbo' AND state IN ('active','pending','unknown','manual_attention')),COUNT(*) FILTER (WHERE mode='hybrid' AND state IN ('active','pending','unknown','manual_attention')),COUNT(*) FILTER (WHERE mode='split' AND state IN ('active','pending','unknown','manual_attention')) FROM mobile_fulfillment_plans WHERE organization_id=$1 AND workspace_id=$2`, s.OrganizationID(), s.WorkspaceID()).Scan(&out.ActivePlans, &out.FBSPlans, &out.FBOPlans, &out.HybridPlans, &out.SplitPlans)
	})
	if err != nil {
		return MobileSummary{}, err
	}
	err = r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE state IN ('pending','in_progress')),COUNT(*) FILTER (WHERE state='exception') FROM wms_execution_tasks WHERE organization_id=$1 AND workspace_id=$2`, s.OrganizationID(), s.WorkspaceID()).Scan(&out.PendingTasks, &out.ExceptionTasks)
	})
	if err != nil {
		return MobileSummary{}, err
	}
	err = r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE state='queued') FROM mobile_print_jobs WHERE organization_id=$1 AND workspace_id=$2`, s.OrganizationID(), s.WorkspaceID()).Scan(&out.PendingPrints)
	})
	if err != nil {
		return MobileSummary{}, err
	}
	return out, r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mobile_offline_intents WHERE organization_id=$1 AND workspace_id=$2 AND state='pending'`, s.OrganizationID(), s.WorkspaceID()).Scan(&out.OfflinePending)
	})
}

func validMobilePlanCommand(command CreateMobilePlan) bool {
	if command.OrderID != "" && !validSortableIDValue(command.OrderID) {
		return false
	}
	if command.WarehouseID != "" && !validSortableIDValue(command.WarehouseID) {
		return false
	}
	return inventory.ValidateMobilePlan(command.Mode, command.Owner, command.LocalExecution, command.WarehouseID) == nil && (command.LocalExecution == (command.Mode != inventory.FulfillmentFBO))
}

func sameMobilePlan(existing MobilePlan, command CreateMobilePlan) bool {
	return existing.OrderID == command.OrderID && existing.WarehouseID == command.WarehouseID && existing.Mode == command.Mode && existing.Owner == command.Owner && existing.LocalExecution == command.LocalExecution && existing.RemoteReferenceDigest == command.RemoteReferenceDigest
}

func validMobileBatchStrategy(value string) bool {
	return value == "order" || value == "wave" || value == "zone" || value == "batch"
}

func validMobileTaskIDs(values []string) bool {
	if len(values) < 1 || len(values) > 100 {
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

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func validMobilePlanState(value string) bool {
	switch value {
	case "active", "pending", "unknown", "complete", "cancelled", "manual_attention":
		return true
	default:
		return false
	}
}

func validMobilePlanStage(value string) bool {
	switch value {
	case "pick", "pack", "print", "handoff", "tracking", "remote_visibility", "complete", "manual_attention":
		return true
	default:
		return false
	}
}

func mobilePlanStageRank(value string) int {
	switch value {
	case "pick":
		return 1
	case "pack":
		return 2
	case "print":
		return 3
	case "handoff":
		return 4
	case "tracking", "remote_visibility":
		return 5
	case "complete":
		return 6
	default:
		return 0
	}
}

func validMobilePrintState(value string) bool {
	switch value {
	case "queued", "printed", "failed", "unknown", "cancelled":
		return true
	default:
		return false
	}
}

func validMobileDeviceKind(value string) bool {
	return value == "handheld" || value == "camera" || value == "scanner_station" || value == "scale_station"
}

func validMobileOperation(value inventory.MobileOperation) bool {
	switch value {
	case inventory.MobileOperationPick, inventory.MobileOperationPack, inventory.MobileOperationPrint, inventory.MobileOperationHandoff, inventory.MobileOperationScan:
		return true
	default:
		return false
	}
}

func validMobileIntentState(value string) bool {
	return value == "pending" || value == "applied" || value == "rejected" || value == "unknown" || value == "needs_attention"
}

func validMobileObservationStage(value string) bool {
	return value == "remote_visibility" || value == "handoff" || value == "tracking" || value == "complete" || value == "manual_attention"
}

func validMobileObservationState(value string) bool {
	return value == "pending" || value == "observed" || value == "accepted" || value == "unknown" || value == "manual_attention"
}

func isMobileResult(value string) bool {
	return value == "applied" || value == "rejected" || value == "unknown" || value == "pending"
}

func loadMobilePlan(ctx context.Context, tx *sql.Tx, s inventory.Scope, id string, lock bool) (MobilePlan, error) {
	query := mobilePlanSelect + ` AND plan_id=$3`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanMobilePlan(tx.QueryRowContext(ctx, query, s.OrganizationID(), s.WorkspaceID(), id))
}

func scanMobilePlan(row scanner) (MobilePlan, error) {
	var out MobilePlan
	if err := row.Scan(&out.ID, &out.IdempotencyKey, &out.OrderID, &out.WarehouseID, &out.Mode, &out.Owner, &out.Stage, &out.State, &out.LocalExecution, &out.RemoteReferenceDigest, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MobilePlan{}, inventory.ErrNotFound
		}
		return MobilePlan{}, err
	}
	if !validSortableIDValue(out.ID) || !inventory.MobileFulfillmentModeValid(out.Mode) || out.Version < 1 || !validMobilePlanState(out.State) {
		return MobilePlan{}, inventory.ErrInvalidRecord
	}
	out.CreatedAt, out.UpdatedAt = out.CreatedAt.UTC(), out.UpdatedAt.UTC()
	return out, nil
}

func loadMobilePickBatch(ctx context.Context, tx *sql.Tx, s inventory.Scope, id string, lock bool) (MobilePickBatch, error) {
	query := mobileBatchSelect + ` AND batch_id=$3`
	if lock {
		query += ` FOR UPDATE`
	}
	out, err := scanMobilePickBatch(tx.QueryRowContext(ctx, query, s.OrganizationID(), s.WorkspaceID(), id))
	if err != nil {
		return MobilePickBatch{}, err
	}
	out.TaskIDs, err = loadMobileBatchTaskIDs(ctx, tx, s, id)
	return out, err
}

func scanMobilePickBatch(row scanner) (MobilePickBatch, error) {
	var out MobilePickBatch
	if err := row.Scan(&out.ID, &out.IdempotencyKey, &out.PlanID, &out.WarehouseID, &out.Strategy, &out.State, &out.RouteDigest, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MobilePickBatch{}, inventory.ErrNotFound
		}
		return MobilePickBatch{}, err
	}
	out.CreatedAt, out.UpdatedAt = out.CreatedAt.UTC(), out.UpdatedAt.UTC()
	return out, nil
}

func loadMobileBatchTaskIDs(ctx context.Context, tx *sql.Tx, s inventory.Scope, id string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT task_id FROM mobile_pick_batch_tasks WHERE organization_id=$1 AND workspace_id=$2 AND batch_id=$3 ORDER BY position`, s.OrganizationID(), s.WorkspaceID(), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]string, 0, 100)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, rows.Err()
}

func sameMobilePickBatch(existing MobilePickBatch, command CreateMobilePickBatch) bool {
	if existing.PlanID != command.PlanID || existing.WarehouseID != command.WarehouseID || existing.Strategy != command.Strategy || existing.RouteDigest != command.RouteDigest || len(existing.TaskIDs) != len(command.TaskIDs) {
		return false
	}
	for i := range command.TaskIDs {
		if existing.TaskIDs[i] != command.TaskIDs[i] {
			return false
		}
	}
	return true
}

func loadMobileScan(ctx context.Context, tx *sql.Tx, s inventory.Scope, id string) (MobileScanEvidence, error) {
	row := tx.QueryRowContext(ctx, `SELECT scan_id,idempotency_key,plan_id,task_id,device_id,kind,code_digest,location_code,quantity_coefficient,quantity_scale,unit,result,occurred_at FROM mobile_scan_evidence WHERE organization_id=$1 AND workspace_id=$2 AND scan_id=$3`, s.OrganizationID(), s.WorkspaceID(), id)
	var out MobileScanEvidence
	var coefficient int64
	var scale int16
	if err := row.Scan(&out.ID, &out.IdempotencyKey, &out.PlanID, &out.TaskID, &out.DeviceID, &out.Kind, &out.CodeDigest, &out.LocationCode, &coefficient, &scale, &out.Quantity.Unit, &out.Result, &out.OccurredAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MobileScanEvidence{}, inventory.ErrNotFound
		}
		return MobileScanEvidence{}, err
	}
	decimal, err := inventory.NewDecimal(coefficient, uint8(scale))
	if err != nil {
		return MobileScanEvidence{}, inventory.ErrInvalidRecord
	}
	out.Quantity, err = inventory.NewQuantity(decimal, out.Quantity.Unit)
	if err != nil {
		return MobileScanEvidence{}, inventory.ErrInvalidRecord
	}
	out.OccurredAt = out.OccurredAt.UTC()
	return out, nil
}

func loadMobilePack(ctx context.Context, tx *sql.Tx, s inventory.Scope, id string) (MobilePackSession, error) {
	return scanMobilePack(tx.QueryRowContext(ctx, mobilePackSelect+` AND pack_id=$3`, s.OrganizationID(), s.WorkspaceID(), id))
}

func scanMobilePack(row scanner) (MobilePackSession, error) {
	var out MobilePackSession
	if err := row.Scan(&out.ID, &out.IdempotencyKey, &out.PlanID, &out.BatchID, &out.Facts.PackageCount, &out.Facts.WeightGrams, &out.Facts.LengthMM, &out.Facts.WidthMM, &out.Facts.HeightMM, &out.PackagingType, &out.State, &out.FactsDigest, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MobilePackSession{}, inventory.ErrNotFound
		}
		return MobilePackSession{}, err
	}
	out.CreatedAt, out.UpdatedAt = out.CreatedAt.UTC(), out.UpdatedAt.UTC()
	return out, nil
}

func sameMobilePack(existing MobilePackSession, command CreateMobilePack) bool {
	return existing.PlanID == command.PlanID && existing.BatchID == command.BatchID && existing.Facts == command.Facts && existing.PackagingType == command.PackagingType && existing.FactsDigest == command.FactsDigest
}

func sameMobilePrint(existing MobilePrintJob, command QueueMobilePrint) bool {
	return existing.PlanID == command.PlanID && existing.PackID == command.PackID && existing.PrinterID == command.PrinterID && existing.Document == command.Document && existing.TemplateVersion == command.TemplateVersion && existing.MediaSize == command.MediaSize && existing.Language == command.Language && existing.Copies == command.Copies && existing.Reprint == command.Reprint && existing.ReasonCode == command.ReasonCode && existing.ArtifactDigest == command.ArtifactDigest
}

func loadMobilePrint(ctx context.Context, tx *sql.Tx, s inventory.Scope, id string) (MobilePrintJob, error) {
	return scanMobilePrint(tx.QueryRowContext(ctx, mobilePrintSelect+` AND print_job_id=$3`, s.OrganizationID(), s.WorkspaceID(), id))
}

func scanMobilePrint(row scanner) (MobilePrintJob, error) {
	var out MobilePrintJob
	if err := row.Scan(&out.ID, &out.IdempotencyKey, &out.PlanID, &out.PackID, &out.PrinterID, &out.Document, &out.TemplateVersion, &out.MediaSize, &out.Language, &out.Copies, &out.State, &out.Reprint, &out.ReasonCode, &out.ArtifactDigest, &out.ErrorCode, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MobilePrintJob{}, inventory.ErrNotFound
		}
		return MobilePrintJob{}, err
	}
	out.CreatedAt, out.UpdatedAt = out.CreatedAt.UTC(), out.UpdatedAt.UTC()
	return out, nil
}

func loadMobileDevice(ctx context.Context, tx *sql.Tx, s inventory.Scope, id string) (MobileDevice, error) {
	return scanMobileDevice(tx.QueryRowContext(ctx, mobileDeviceSelect+` AND device_id=$3`, s.OrganizationID(), s.WorkspaceID(), id))
}

func scanMobileDevice(row scanner) (MobileDevice, error) {
	var out MobileDevice
	if err := row.Scan(&out.ID, &out.IdempotencyKey, &out.Label, &out.WarehouseID, &out.ZoneCode, &out.Kind, &out.State, &out.OperatorID, &out.LastSeenAt, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MobileDevice{}, inventory.ErrNotFound
		}
		return MobileDevice{}, err
	}
	if out.LastSeenAt != nil {
		value := out.LastSeenAt.UTC()
		out.LastSeenAt = &value
	}
	out.CreatedAt, out.UpdatedAt = out.CreatedAt.UTC(), out.UpdatedAt.UTC()
	return out, nil
}

func loadMobileIntent(ctx context.Context, tx *sql.Tx, s inventory.Scope, id string) (MobileOfflineIntent, error) {
	return scanMobileIntent(tx.QueryRowContext(ctx, mobileIntentSelect+` AND intent_id=$3`, s.OrganizationID(), s.WorkspaceID(), id))
}

func scanMobileIntent(row scanner) (MobileOfflineIntent, error) {
	var out MobileOfflineIntent
	if err := row.Scan(&out.ID, &out.IdempotencyKey, &out.DeviceID, &out.PlanID, &out.Operation, &out.PayloadDigest, &out.SequenceNo, &out.State, &out.ErrorCode, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MobileOfflineIntent{}, inventory.ErrNotFound
		}
		return MobileOfflineIntent{}, err
	}
	out.CreatedAt, out.UpdatedAt = out.CreatedAt.UTC(), out.UpdatedAt.UTC()
	return out, nil
}

func enqueueMobilePlanEvent(ctx context.Context, tx *sql.Tx, s inventory.Scope, mutation inventory.Mutation, plan MobilePlan, change string) error {
	payload, err := json.Marshal(struct {
		PlanID         string `json:"plan_id"`
		Mode           string `json:"mode"`
		Owner          string `json:"owner"`
		Stage          string `json:"stage"`
		State          string `json:"state"`
		WarehouseID    string `json:"warehouse_id"`
		LocalExecution bool   `json:"local_execution"`
		Version        int64  `json:"version"`
		Change         string `json:"change"`
	}{plan.ID, string(plan.Mode), string(plan.Owner), plan.Stage, plan.State, plan.WarehouseID, plan.LocalExecution, plan.Version, change})
	if err != nil {
		return err
	}
	event, err := eventBase(s, mutation, "commerce.fulfillment.mobile_task_changed.v1", "mobile_fulfillment_plan", plan.ID, payload)
	if err != nil {
		return err
	}
	return enqueue(ctx, tx, event)
}

func enqueueMobileBatchEvent(ctx context.Context, tx *sql.Tx, s inventory.Scope, mutation inventory.Mutation, batch MobilePickBatch, change string) error {
	payload, err := json.Marshal(struct {
		BatchID     string   `json:"batch_id"`
		PlanID      string   `json:"plan_id"`
		WarehouseID string   `json:"warehouse_id"`
		Strategy    string   `json:"strategy"`
		State       string   `json:"state"`
		TaskIDs     []string `json:"task_ids"`
		Version     int64    `json:"version"`
		Change      string   `json:"change"`
	}{batch.ID, batch.PlanID, batch.WarehouseID, batch.Strategy, batch.State, batch.TaskIDs, batch.Version, change})
	if err != nil {
		return err
	}
	event, err := eventBase(s, mutation, "commerce.fulfillment.mobile_task_changed.v1", "mobile_pick_batch", batch.ID, payload)
	if err != nil {
		return err
	}
	return enqueue(ctx, tx, event)
}

func enqueueMobileScanEvent(ctx context.Context, tx *sql.Tx, s inventory.Scope, mutation inventory.Mutation, scan MobileScanEvidence) error {
	payload, err := json.Marshal(struct {
		ScanID       string          `json:"scan_id"`
		PlanID       string          `json:"plan_id"`
		TaskID       string          `json:"task_id"`
		DeviceID     string          `json:"device_id"`
		Kind         string          `json:"kind"`
		CodeDigest   string          `json:"code_digest"`
		LocationCode string          `json:"location_code"`
		Result       string          `json:"result"`
		Quantity     wmsWireQuantity `json:"quantity"`
	}{scan.ID, scan.PlanID, scan.TaskID, scan.DeviceID, string(scan.Kind), scan.CodeDigest, scan.LocationCode, scan.Result, wmsWireQuantity{scan.Quantity.Value.String(), scan.Quantity.Unit.String()}})
	if err != nil {
		return err
	}
	event, err := eventBase(s, mutation, "commerce.fulfillment.mobile_scan_recorded.v1", "mobile_scan_evidence", scan.ID, payload)
	if err != nil {
		return err
	}
	return enqueue(ctx, tx, event)
}

func enqueueMobilePackEvent(ctx context.Context, tx *sql.Tx, s inventory.Scope, mutation inventory.Mutation, pack MobilePackSession, change string) error {
	payload, err := json.Marshal(struct {
		PackID       string `json:"pack_id"`
		PlanID       string `json:"plan_id"`
		BatchID      string `json:"batch_id"`
		State        string `json:"state"`
		FactsDigest  string `json:"facts_digest"`
		PackageCount int    `json:"package_count"`
		WeightGrams  int64  `json:"weight_grams"`
		LengthMM     int64  `json:"length_mm"`
		WidthMM      int64  `json:"width_mm"`
		HeightMM     int64  `json:"height_mm"`
		Version      int64  `json:"version"`
		Change       string `json:"change"`
	}{pack.ID, pack.PlanID, pack.BatchID, pack.State, pack.FactsDigest, pack.Facts.PackageCount, pack.Facts.WeightGrams, pack.Facts.LengthMM, pack.Facts.WidthMM, pack.Facts.HeightMM, pack.Version, change})
	if err != nil {
		return err
	}
	event, err := eventBase(s, mutation, "commerce.fulfillment.mobile_task_changed.v1", "mobile_pack_session", pack.ID, payload)
	if err != nil {
		return err
	}
	return enqueue(ctx, tx, event)
}

func enqueueMobilePrintEvent(ctx context.Context, tx *sql.Tx, s inventory.Scope, mutation inventory.Mutation, job MobilePrintJob, change string) error {
	payload, err := json.Marshal(struct {
		PrintJobID string `json:"print_job_id"`
		PlanID     string `json:"plan_id"`
		PackID     string `json:"pack_id"`
		PrinterID  string `json:"printer_id"`
		Document   string `json:"document"`
		State      string `json:"state"`
		ErrorCode  string `json:"error_code"`
		Copies     int    `json:"copies"`
		Reprint    bool   `json:"reprint"`
		Version    int64  `json:"version"`
		Change     string `json:"change"`
	}{job.ID, job.PlanID, job.PackID, job.PrinterID, string(job.Document), job.State, job.ErrorCode, job.Copies, job.Reprint, job.Version, change})
	if err != nil {
		return err
	}
	event, err := eventBase(s, mutation, "commerce.fulfillment.mobile_print_job_changed.v1", "mobile_print_job", job.ID, payload)
	if err != nil {
		return err
	}
	return enqueue(ctx, tx, event)
}

func mobilePayloadDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
