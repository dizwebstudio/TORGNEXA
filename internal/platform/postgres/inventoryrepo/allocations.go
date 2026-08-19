package inventoryrepo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/inventory"
)

const allocationSelect = `SELECT allocation_id,idempotency_key,order_id,order_item_id,offer_id,warehouse_id,quantity_coefficient,quantity_scale,unit,state,COALESCE(reason_code,''),COALESCE(incident_id,''),COALESCE(replaces_allocation_id,''),version,created_at,updated_at FROM fulfillment_allocations WHERE organization_id=$1 AND workspace_id=$2`

func (r *Repository) ReserveOrderItem(ctx context.Context, s inventory.Scope, command inventory.ReserveOrderItem, mutation inventory.Mutation) (inventory.FulfillmentAllocation, error) {
	if err := validateMutation(ctx, r, s, mutation); err != nil {
		return inventory.FulfillmentAllocation{}, err
	}
	if command.Validate() != nil {
		return inventory.FulfillmentAllocation{}, inventory.ErrInvalidRecord
	}
	var out inventory.FulfillmentAllocation
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var existing inventory.FulfillmentAllocation
		err := scanAllocation(tx.QueryRowContext(ctx, allocationSelect+` AND idempotency_key=$3`, s.OrganizationID(), s.WorkspaceID(), command.IdempotencyKey), &existing)
		if err == nil {
			if existing.OrderItemID != command.OrderItemID || existing.WarehouseID != command.WarehouseID {
				return inventory.ErrConflict
			}
			out = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		var orderID, offerID, unit, orderStatus string
		var coefficient int64
		var scale uint8
		if err := tx.QueryRowContext(ctx, `SELECT i.order_id,i.offer_id,i.quantity_coefficient,i.quantity_scale,i.unit,o.status FROM order_items i JOIN orders o ON o.organization_id=i.organization_id AND o.workspace_id=i.workspace_id AND o.id=i.order_id WHERE i.organization_id=$1 AND i.workspace_id=$2 AND i.id=$3`, s.OrganizationID(), s.WorkspaceID(), command.OrderItemID).Scan(&orderID, &offerID, &coefficient, &scale, &unit, &orderStatus); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return inventory.ErrNotFound
			}
			return err
		}
		if orderStatus == "fulfilled" || orderStatus == "cancelled" {
			return inventory.ErrInvalidRecord
		}
		quantityDecimal, err := inventory.NewDecimal(coefficient, scale)
		if err != nil {
			return err
		}
		unitCode, err := inventory.NewUnitCode(unit)
		if err != nil {
			return err
		}
		quantity, _ := inventory.NewQuantity(quantityDecimal, unitCode)
		position, err := positionByParentsForUpdate(ctx, tx, s, inventory.OfferID(offerID), command.WarehouseID)
		if err != nil {
			return err
		}
		if position.OnHand.Unit != quantity.Unit {
			return inventory.ErrInvalidRecord
		}
		if err := requireWarehouseAllocatable(ctx, tx, s, command.WarehouseID); err != nil {
			return err
		}
		available, err := position.Available()
		if err != nil {
			return err
		}
		cmp, err := available.Value.Cmp(quantity.Value)
		if err != nil || cmp < 0 {
			return inventory.ErrInsufficientAvailable
		}
		newReserved, err := position.Reserved.Value.Add(quantity.Value)
		if err != nil {
			return err
		}
		updated, err := updateReservedTx(ctx, tx, s, position, newReserved, mutation, "reserved", "order_fulfillment")
		if err != nil {
			return err
		}
		_ = updated
		now := mutation.OccurredAt.UTC()
		if err := scanAllocation(tx.QueryRowContext(ctx, `INSERT INTO fulfillment_allocations(organization_id,workspace_id,allocation_id,idempotency_key,order_id,order_item_id,offer_id,warehouse_id,quantity_coefficient,quantity_scale,unit,state,reason_code,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'reserved','order_fulfillment',1,$12,$12) RETURNING allocation_id,idempotency_key,order_id,order_item_id,offer_id,warehouse_id,quantity_coefficient,quantity_scale,unit,state,COALESCE(reason_code,''),COALESCE(incident_id,''),COALESCE(replaces_allocation_id,''),version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), command.AllocationID, command.IdempotencyKey, orderID, command.OrderItemID, offerID, command.WarehouseID.String(), coefficient, scale, unit, now), &out); err != nil {
			return err
		}
		return enqueueAllocationEvent(ctx, tx, s, derivedMutation(mutation, "allocation"), out, "order_fulfillment")
	})
	return out, err
}

func (r *Repository) FulfillmentAllocation(ctx context.Context, s inventory.Scope, allocationID string) (inventory.FulfillmentAllocation, error) {
	if err := validate(ctx, r, s); err != nil || !validSortableIDValue(allocationID) {
		return inventory.FulfillmentAllocation{}, inventory.ErrInvalidRecord
	}
	var out inventory.FulfillmentAllocation
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		return scanAllocation(tx.QueryRowContext(ctx, allocationSelect+` AND allocation_id=$3`, s.OrganizationID(), s.WorkspaceID(), allocationID), &out)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return inventory.FulfillmentAllocation{}, inventory.ErrNotFound
	}
	return out, err
}

func (r *Repository) ListFulfillmentAllocations(ctx context.Context, s inventory.Scope, limit int) ([]inventory.FulfillmentAllocation, error) {
	if err := validate(ctx, r, s); err != nil || limit < 1 || limit > 500 {
		return nil, inventory.ErrInvalidRecord
	}
	out := make([]inventory.FulfillmentAllocation, 0, limit)
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, allocationSelect+` ORDER BY updated_at DESC,allocation_id DESC LIMIT $3`, s.OrganizationID(), s.WorkspaceID(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item inventory.FulfillmentAllocation
			if err := scanAllocation(rows, &item); err != nil {
				return err
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}

func requireWarehouseAllocatable(ctx context.Context, tx *sql.Tx, s inventory.Scope, warehouse inventory.WarehouseID) error {
	var admin, operational string
	if err := tx.QueryRowContext(ctx, `SELECT w.status,COALESCE(st.state,'active') FROM warehouses w LEFT JOIN warehouse_operational_state st ON st.organization_id=w.organization_id AND st.workspace_id=w.workspace_id AND st.warehouse_id=w.id WHERE w.organization_id=$1 AND w.workspace_id=$2 AND w.id=$3`, s.OrganizationID(), s.WorkspaceID(), warehouse.String()).Scan(&admin, &operational); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return inventory.ErrNotFound
		}
		return err
	}
	if admin != "active" || (operational != "active" && operational != "degraded") {
		return inventory.ErrWarehouseInactive
	}
	return nil
}

func positionByParentsForUpdate(ctx context.Context, tx *sql.Tx, s inventory.Scope, offer inventory.OfferID, warehouse inventory.WarehouseID) (inventory.Position, error) {
	return scanPosition(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,offer_id,warehouse_id,on_hand_coefficient,on_hand_scale,reserved_coefficient,reserved_scale,unit,version,created_at,updated_at FROM inventory_positions WHERE organization_id=$1 AND workspace_id=$2 AND offer_id=$3 AND warehouse_id=$4 FOR UPDATE`, s.OrganizationID(), s.WorkspaceID(), offer.String(), warehouse.String()))
}

func updateReservedTx(ctx context.Context, tx *sql.Tx, s inventory.Scope, current inventory.Position, reserved inventory.Decimal, mutation inventory.Mutation, change, reason string) (inventory.Position, error) {
	quantity, err := inventory.NewQuantity(reserved, current.Reserved.Unit)
	if err != nil {
		return inventory.Position{}, err
	}
	cmpZero, _ := quantity.Value.Cmp(mustDecimalZero())
	cmpOnHand, _ := quantity.Value.Cmp(current.OnHand.Value)
	if cmpZero < 0 {
		return inventory.Position{}, inventory.ErrInsufficientReserved
	}
	if cmpOnHand > 0 {
		return inventory.Position{}, inventory.ErrInsufficientAvailable
	}
	out, err := scanPosition(tx.QueryRowContext(ctx, `UPDATE inventory_positions SET reserved_coefficient=$4,reserved_scale=$5,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$6 RETURNING id,organization_id,workspace_id,offer_id,warehouse_id,on_hand_coefficient,on_hand_scale,reserved_coefficient,reserved_scale,unit,version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), current.ID.String(), quantity.Value.Coefficient(), quantity.Value.Scale(), current.Version))
	if err != nil {
		return inventory.Position{}, err
	}
	if err := appendPositionAudit(ctx, tx, s, mutation, out, "inventory.position."+change, &current, change, reason); err != nil {
		return inventory.Position{}, err
	}
	if err := enqueuePositionEvent(ctx, tx, s, mutation, out, change, reason); err != nil {
		return inventory.Position{}, err
	}
	if err := appendPositionLineage(ctx, tx, s, mutation, out, &current, change); err != nil {
		return inventory.Position{}, err
	}
	return out, nil
}

func enqueueAllocationEvent(ctx context.Context, tx *sql.Tx, s inventory.Scope, mutation inventory.Mutation, allocation inventory.FulfillmentAllocation, reason string) error {
	quantity, err := wireQuantity(allocation.Quantity)
	if err != nil {
		return err
	}
	payload := struct {
		AllocationID string      `json:"allocation_id"`
		OrderID      string      `json:"order_id"`
		OrderItemID  string      `json:"order_item_id"`
		OfferID      string      `json:"offer_id"`
		WarehouseID  string      `json:"warehouse_id"`
		Quantity     interface{} `json:"quantity"`
		Status       string      `json:"status"`
		Version      int64       `json:"version"`
		Reason       string      `json:"reason"`
		IncidentID   string      `json:"incident_id,omitempty"`
		ReplacesID   string      `json:"replaces_allocation_id,omitempty"`
	}{allocation.ID, allocation.OrderID, allocation.OrderItemID, allocation.OfferID.String(), allocation.WarehouseID.String(), quantity, string(allocation.Status), allocation.Version, reason, allocation.IncidentID, allocation.ReplacesID}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ev, err := eventBase(s, mutation, "commerce.fulfillment.allocation_changed.v1", "fulfillment_allocation", allocation.ID, data)
	if err != nil {
		return err
	}
	return enqueue(ctx, tx, ev)
}

func scanAllocation(row rowScanner, out *inventory.FulfillmentAllocation) error {
	if out == nil {
		return errors.New("inventory repository: allocation target required")
	}
	var coefficient int64
	var scale uint8
	var unit string
	if err := row.Scan(allocationScanTargetsRaw(out, &coefficient, &scale, &unit)...); err != nil {
		return err
	}
	decimal, err := inventory.NewDecimal(coefficient, scale)
	if err != nil {
		return inventory.ErrInvalidRecord
	}
	unitCode, err := inventory.NewUnitCode(unit)
	if err != nil {
		return inventory.ErrInvalidRecord
	}
	out.Quantity, _ = inventory.NewQuantity(decimal, unitCode)
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	if out.Validate() != nil {
		return inventory.ErrInvalidRecord
	}
	return nil
}

func allocationScanTargetsRaw(out *inventory.FulfillmentAllocation, coefficient *int64, scale *uint8, unit *string) []any {
	return []any{&out.ID, &out.IdempotencyKey, &out.OrderID, &out.OrderItemID, &out.OfferID, &out.WarehouseID, coefficient, scale, unit, &out.Status, &out.ReasonCode, &out.IncidentID, &out.ReplacesID, &out.Version, &out.CreatedAt, &out.UpdatedAt}
}

func derivedMutation(base inventory.Mutation, suffix string) inventory.Mutation {
	base.EventID = deterministicToken("evt_", base.EventID, suffix)
	return base
}

func deterministicToken(prefix string, parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte{0})
		h.Write([]byte(part))
	}
	return prefix + hex.EncodeToString(h.Sum(nil)[:16])
}

func randomUUIDv7At(now time.Time) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	millis := uint64(now.UTC().UnixMilli())
	value[0], value[1], value[2], value[3], value[4], value[5] = byte(millis>>40), byte(millis>>32), byte(millis>>24), byte(millis>>16), byte(millis>>8), byte(millis)
	value[6], value[8] = (value[6]&0x0f)|0x70, (value[8]&0x3f)|0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func mustDecimalZero() inventory.Decimal {
	v, _ := inventory.NewDecimal(0, 0)
	return v
}

func validSortableIDValue(value string) bool {
	return (inventory.ReserveOrderItem{AllocationID: value, OrderItemID: value, IdempotencyKey: "x", WarehouseID: inventory.WarehouseID(value)}).Validate() == nil
}

type failoverExecutionResult struct {
	Decision            inventory.FailoverDecision
	ExecutionStatus     string
	ExecutionReason     string
	ReroutedAllocations int
	ExecutionAttention  int
}

func executeOfferFailoverTx(ctx context.Context, tx *sql.Tx, s inventory.Scope, incidentID string, source inventory.WarehouseID, offer inventory.OfferID, at time.Time) (failoverExecutionResult, error) {
	allocations, err := activeAllocationsForOfferTx(ctx, tx, s, source, offer)
	if err != nil {
		return failoverExecutionResult{}, err
	}
	if len(allocations) == 0 {
		decision, err := resolveFailoverTx(ctx, tx, s, source, offer, randomFailoverID(), at, false)
		if err != nil {
			return failoverExecutionResult{}, err
		}
		return failoverExecutionResult{Decision: decision, ExecutionStatus: "not_required"}, nil
	}
	terminal, err := allocationsContainTerminalOrderTx(ctx, tx, s, allocations)
	if err != nil {
		return failoverExecutionResult{}, err
	}
	if terminal {
		decision := inventory.FailoverDecision{ID: randomFailoverID(), SourceWarehouseID: source, OfferID: offer, OccurredAt: at.UTC(), ExecutionStatus: "needs_attention", ExecutionReason: "allocation_conflict"}
		return failoverExecutionResult{Decision: decision, ExecutionStatus: "needs_attention", ExecutionReason: "allocation_conflict", ExecutionAttention: 1}, nil
	}

	sourcePosition, err := positionByParentsForUpdate(ctx, tx, s, offer, source)
	if err != nil {
		return failoverExecutionResult{}, err
	}
	total, err := sumAllocationQuantity(allocations, sourcePosition.Reserved.Unit)
	if err != nil {
		return failoverExecutionResult{}, err
	}
	cmp, err := sourcePosition.Reserved.Value.Cmp(total.Value)
	if err != nil {
		return failoverExecutionResult{}, err
	}
	if cmp < 0 {
		decision := inventory.FailoverDecision{ID: randomFailoverID(), SourceWarehouseID: source, OfferID: offer, OccurredAt: at.UTC(), ExecutionStatus: "needs_attention", ExecutionReason: "allocation_conflict"}
		return failoverExecutionResult{Decision: decision, ExecutionStatus: "needs_attention", ExecutionReason: "allocation_conflict", ExecutionAttention: 1}, nil
	}
	untracked, err := sourcePosition.Reserved.Value.Sub(total.Value)
	if err != nil {
		return failoverExecutionResult{}, err
	}
	untrackedCmp, _ := untracked.Cmp(mustDecimalZero())

	destination, err := resolveDestinationForQuantityTx(ctx, tx, s, source, offer, total)
	if errors.Is(err, sql.ErrNoRows) {
		decision := inventory.FailoverDecision{ID: randomFailoverID(), SourceWarehouseID: source, OfferID: offer, OccurredAt: at.UTC(), ExecutionStatus: "needs_attention", ExecutionReason: "insufficient_capacity"}
		return failoverExecutionResult{Decision: decision, ExecutionStatus: "needs_attention", ExecutionReason: "insufficient_capacity", ExecutionAttention: 1}, nil
	}
	if err != nil {
		return failoverExecutionResult{}, err
	}
	destinationPosition, err := positionByParentsForUpdate(ctx, tx, s, offer, destination)
	if err != nil {
		return failoverExecutionResult{}, err
	}
	available, err := destinationPosition.Available()
	if err != nil {
		return failoverExecutionResult{}, err
	}
	capacity, err := available.Value.Cmp(total.Value)
	if err != nil || capacity < 0 {
		return failoverExecutionResult{}, inventory.ErrInsufficientAvailable
	}

	sourceReserved, err := sourcePosition.Reserved.Value.Sub(total.Value)
	if err != nil {
		return failoverExecutionResult{}, err
	}
	destinationReserved, err := destinationPosition.Reserved.Value.Add(total.Value)
	if err != nil {
		return failoverExecutionResult{}, err
	}
	sourceMutation, err := failoverMutation(incidentID, offer.String(), "source_release", at)
	if err != nil {
		return failoverExecutionResult{}, err
	}
	destinationMutation, err := failoverMutation(incidentID, offer.String(), "destination_reserve", at)
	if err != nil {
		return failoverExecutionResult{}, err
	}
	if _, err := updateReservedTx(ctx, tx, s, sourcePosition, sourceReserved, sourceMutation, "released", "warehouse_failover"); err != nil {
		return failoverExecutionResult{}, err
	}
	if _, err := updateReservedTx(ctx, tx, s, destinationPosition, destinationReserved, destinationMutation, "reserved", "warehouse_failover"); err != nil {
		return failoverExecutionResult{}, err
	}

	for _, sourceAllocation := range allocations {
		var released inventory.FulfillmentAllocation
		if err := scanAllocation(tx.QueryRowContext(ctx, `UPDATE fulfillment_allocations SET state='released',reason_code='warehouse_failover',version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND allocation_id=$3 AND state='reserved' RETURNING allocation_id,idempotency_key,order_id,order_item_id,offer_id,warehouse_id,quantity_coefficient,quantity_scale,unit,state,COALESCE(reason_code,''),COALESCE(incident_id,''),COALESCE(replaces_allocation_id,''),version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), sourceAllocation.ID), &released); err != nil {
			return failoverExecutionResult{}, err
		}
		newID, err := randomUUIDv7At(at)
		if err != nil {
			return failoverExecutionResult{}, err
		}
		idempotency := deterministicToken("failover_", incidentID, sourceAllocation.ID)
		var replacement inventory.FulfillmentAllocation
		if err := scanAllocation(tx.QueryRowContext(ctx, `INSERT INTO fulfillment_allocations(organization_id,workspace_id,allocation_id,idempotency_key,order_id,order_item_id,offer_id,warehouse_id,quantity_coefficient,quantity_scale,unit,state,reason_code,incident_id,replaces_allocation_id,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'reserved','warehouse_failover',$12,$13,1,$14,$14) RETURNING allocation_id,idempotency_key,order_id,order_item_id,offer_id,warehouse_id,quantity_coefficient,quantity_scale,unit,state,COALESCE(reason_code,''),COALESCE(incident_id,''),COALESCE(replaces_allocation_id,''),version,created_at,updated_at`, s.OrganizationID(), s.WorkspaceID(), newID, idempotency, sourceAllocation.OrderID, sourceAllocation.OrderItemID, sourceAllocation.OfferID.String(), destination.String(), sourceAllocation.Quantity.Value.Coefficient(), sourceAllocation.Quantity.Value.Scale(), sourceAllocation.Quantity.Unit.String(), incidentID, sourceAllocation.ID, at.UTC()), &replacement); err != nil {
			return failoverExecutionResult{}, err
		}
		baseMutation, err := failoverMutation(incidentID, sourceAllocation.ID, "allocation", at)
		if err != nil {
			return failoverExecutionResult{}, err
		}
		if err := enqueueAllocationEvent(ctx, tx, s, derivedMutation(baseMutation, "released"), released, "warehouse_failover"); err != nil {
			return failoverExecutionResult{}, err
		}
		if err := enqueueAllocationEvent(ctx, tx, s, derivedMutation(baseMutation, "reserved"), replacement, "warehouse_failover"); err != nil {
			return failoverExecutionResult{}, err
		}
	}

	status, reason, attention := "rerouted", "", 0
	if untrackedCmp > 0 {
		status, reason, attention = "needs_attention", "untracked_reservation", 1
	}
	decision := inventory.FailoverDecision{ID: randomFailoverID(), SourceWarehouseID: source, DestinationWarehouseID: destination, OfferID: offer, Routed: true, ExecutionStatus: status, ExecutionReason: reason, ReroutedAllocations: len(allocations), OccurredAt: at.UTC()}
	return failoverExecutionResult{Decision: decision, ExecutionStatus: status, ExecutionReason: reason, ReroutedAllocations: len(allocations), ExecutionAttention: attention}, nil
}

func activeAllocationsForOfferTx(ctx context.Context, tx *sql.Tx, s inventory.Scope, warehouse inventory.WarehouseID, offer inventory.OfferID) ([]inventory.FulfillmentAllocation, error) {
	rows, err := tx.QueryContext(ctx, allocationSelect+` AND warehouse_id=$3 AND offer_id=$4 AND state='reserved' ORDER BY order_item_id,allocation_id FOR UPDATE`, s.OrganizationID(), s.WorkspaceID(), warehouse.String(), offer.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []inventory.FulfillmentAllocation
	for rows.Next() {
		var item inventory.FulfillmentAllocation
		if err := scanAllocation(rows, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func allocationsContainTerminalOrderTx(ctx context.Context, tx *sql.Tx, s inventory.Scope, values []inventory.FulfillmentAllocation) (bool, error) {
	if len(values) == 0 {
		return false, nil
	}
	seen := make(map[string]struct{}, len(values))
	for _, allocation := range values {
		if _, ok := seen[allocation.OrderID]; ok {
			continue
		}
		seen[allocation.OrderID] = struct{}{}
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM orders WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 FOR SHARE`, s.OrganizationID(), s.WorkspaceID(), allocation.OrderID).Scan(&status); err != nil {
			return false, err
		}
		if status == "fulfilled" || status == "cancelled" {
			return true, nil
		}
	}
	return false, nil
}

func sumAllocationQuantity(values []inventory.FulfillmentAllocation, unit inventory.UnitCode) (inventory.Quantity, error) {
	total := mustDecimalZero()
	for _, value := range values {
		if value.Quantity.Unit != unit {
			return inventory.Quantity{}, inventory.ErrInvalidRecord
		}
		next, err := total.Add(value.Quantity.Value)
		if err != nil {
			return inventory.Quantity{}, err
		}
		total = next
	}
	return inventory.NewQuantity(total, unit)
}

func resolveDestinationForQuantityTx(ctx context.Context, tx *sql.Tx, s inventory.Scope, source inventory.WarehouseID, offer inventory.OfferID, quantity inventory.Quantity) (inventory.WarehouseID, error) {
	var destination inventory.WarehouseID
	err := tx.QueryRowContext(ctx, `SELECT r.destination_warehouse_id FROM warehouse_failover_routes r JOIN warehouses w ON w.organization_id=r.organization_id AND w.workspace_id=r.workspace_id AND w.id=r.destination_warehouse_id LEFT JOIN warehouse_operational_state st ON st.organization_id=r.organization_id AND st.workspace_id=r.workspace_id AND st.warehouse_id=r.destination_warehouse_id JOIN inventory_positions p ON p.organization_id=r.organization_id AND p.workspace_id=r.workspace_id AND p.warehouse_id=r.destination_warehouse_id AND p.offer_id=$4 WHERE r.organization_id=$1 AND r.workspace_id=$2 AND r.source_warehouse_id=$3 AND r.enabled AND w.status='active' AND COALESCE(st.state,'active') IN ('active','degraded') AND p.unit=$7 AND (p.on_hand_coefficient::numeric / power(10::numeric,p.on_hand_scale) - p.reserved_coefficient::numeric / power(10::numeric,p.reserved_scale)) >= ($5::numeric / power(10::numeric,$6)) ORDER BY CASE COALESCE(st.state,'active') WHEN 'active' THEN 0 ELSE 1 END,r.priority,r.destination_warehouse_id LIMIT 1`, s.OrganizationID(), s.WorkspaceID(), source.String(), offer.String(), quantity.Value.Coefficient(), quantity.Value.Scale(), quantity.Unit.String()).Scan(&destination)
	return destination, err
}

func failoverMutation(incidentID, entityID, phase string, at time.Time) (inventory.Mutation, error) {
	auditID, err := randomUUIDv7At(at)
	if err != nil {
		return inventory.Mutation{}, err
	}
	return inventory.Mutation{
		EventID:       deterministicToken("evt_", incidentID, entityID, phase),
		AuditID:       auditID,
		ActorID:       "warehouse-failover",
		Source:        "worker",
		CorrelationID: incidentID,
		CausationID:   incidentID,
		OccurredAt:    at.UTC(),
	}, nil
}
