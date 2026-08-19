package inventoryrepo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/inventory"
)

var reasonCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func (r *Repository) OperationalState(ctx context.Context, s inventory.Scope, warehouse inventory.WarehouseID) (inventory.WarehouseOperationalState, error) {
	if ctx == nil || r == nil || !s.Valid() || !warehouse.Valid() {
		return inventory.WarehouseOperationalState{}, inventory.ErrInvalidRecord
	}
	out := inventory.WarehouseOperationalState{WarehouseID: warehouse, State: inventory.OperationalActive}
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		if _, err := scanWarehouse(tx.QueryRowContext(ctx, warehouseSelect, s.OrganizationID(), s.WorkspaceID(), warehouse.String())); err != nil {
			return err
		}
		err := tx.QueryRowContext(ctx, `SELECT state,COALESCE(reason_code,''),version,changed_at FROM warehouse_operational_state WHERE organization_id=$1 AND workspace_id=$2 AND warehouse_id=$3`, s.OrganizationID(), s.WorkspaceID(), warehouse.String()).Scan(&out.State, &out.ReasonCode, &out.Version, &out.ChangedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil {
		return inventory.WarehouseOperationalState{}, err
	}
	if !out.ChangedAt.IsZero() {
		out.ChangedAt = out.ChangedAt.UTC()
	}
	return out, nil
}

func (r *Repository) SetOperationalState(ctx context.Context, s inventory.Scope, warehouse inventory.WarehouseID, state inventory.OperationalState, reason string, expectedVersion int64) (inventory.WarehouseOperationalState, error) {
	if ctx == nil || r == nil || !s.Valid() || !warehouse.Valid() || !state.Valid() || expectedVersion < 0 || (reason != "" && !reasonCodePattern.MatchString(reason)) {
		return inventory.WarehouseOperationalState{}, inventory.ErrInvalidRecord
	}
	var out inventory.WarehouseOperationalState
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var reasonValue any
		if reason != "" {
			reasonValue = reason
		}
		row := tx.QueryRowContext(ctx, `INSERT INTO warehouse_operational_state(organization_id,workspace_id,warehouse_id,state,reason_code,version,changed_at) SELECT $1,$2,$3,$4,$5,1,clock_timestamp() WHERE $6=0 ON CONFLICT(organization_id,workspace_id,warehouse_id) DO UPDATE SET state=EXCLUDED.state,reason_code=EXCLUDED.reason_code,version=warehouse_operational_state.version+1,changed_at=clock_timestamp() WHERE warehouse_operational_state.version=$6 RETURNING warehouse_id,state,COALESCE(reason_code,''),version,changed_at`, s.OrganizationID(), s.WorkspaceID(), warehouse.String(), string(state), reasonValue, expectedVersion)
		if err := row.Scan(&out.WarehouseID, &out.State, &out.ReasonCode, &out.Version, &out.ChangedAt); err != nil {
			return err
		}
		return reconcileWarehouseIncidentState(ctx, tx, s, out)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return inventory.WarehouseOperationalState{}, inventory.ErrConflict
	}
	out.ChangedAt = out.ChangedAt.UTC()
	return out, err
}

func reconcileWarehouseIncidentState(ctx context.Context, tx *sql.Tx, s inventory.Scope, state inventory.WarehouseOperationalState) error {
	if state.State != inventory.OperationalUnavailable && state.State != inventory.OperationalLost {
		_, err := tx.ExecContext(ctx, `UPDATE warehouse_incidents SET status='resolved',completed_at=COALESCE(completed_at,clock_timestamp()),updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND warehouse_id=$3 AND status IN ('open','processing','needs_attention')`, s.OrganizationID(), s.WorkspaceID(), state.WarehouseID.String())
		return tolerateMissingIncidentSchema(err)
	}
	// A repeated hard-down transition represents a new incident. Resolve any
	// previous active/attention record before opening a fresh immutable identity.
	if _, err := tx.ExecContext(ctx, `UPDATE warehouse_incidents SET status='resolved',completed_at=COALESCE(completed_at,clock_timestamp()),updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND warehouse_id=$3 AND status IN ('open','processing','needs_attention')`, s.OrganizationID(), s.WorkspaceID(), state.WarehouseID.String()); err != nil {
		return tolerateMissingIncidentSchema(err)
	}
	incidentID, err := randomIncidentID()
	if err != nil {
		return err
	}
	var reason any
	if state.ReasonCode != "" {
		reason = state.ReasonCode
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO warehouse_incidents(organization_id,workspace_id,incident_id,warehouse_id,operational_state,reason_code,status,opened_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,'open',clock_timestamp(),clock_timestamp())`, s.OrganizationID(), s.WorkspaceID(), incidentID, state.WarehouseID.String(), string(state.State), reason)
	return tolerateMissingIncidentSchema(err)
}

type sqlState interface{ SQLState() string }

// During an expand rollout application binaries may reach a database that has
// migration 72 but not 73 yet. Operational state remains safe because migration
// 72 already blocks allocations; incident automation begins as soon as 73 lands.
func tolerateMissingIncidentSchema(err error) error {
	if err == nil {
		return nil
	}
	var state sqlState
	if errors.As(err, &state) && state.SQLState() == "42P01" {
		return nil
	}
	return err
}

func (r *Repository) PutFailoverRoute(ctx context.Context, s inventory.Scope, route inventory.FailoverRoute, expectedVersion int64) (inventory.FailoverRoute, error) {
	if ctx == nil || r == nil || !s.Valid() || !route.SourceWarehouseID.Valid() || !route.DestinationWarehouseID.Valid() || route.SourceWarehouseID == route.DestinationWarehouseID || route.Priority < 1 || route.Priority > 10000 || expectedVersion < 0 {
		return inventory.FailoverRoute{}, inventory.ErrInvalidRecord
	}
	var out inventory.FailoverRoute
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `INSERT INTO warehouse_failover_routes(organization_id,workspace_id,source_warehouse_id,destination_warehouse_id,priority,enabled,version,updated_at) SELECT $1,$2,$3,$4,$5,$6,1,clock_timestamp() WHERE $7=0 ON CONFLICT(organization_id,workspace_id,source_warehouse_id,destination_warehouse_id) DO UPDATE SET priority=EXCLUDED.priority,enabled=EXCLUDED.enabled,version=warehouse_failover_routes.version+1,updated_at=clock_timestamp() WHERE warehouse_failover_routes.version=$7 RETURNING source_warehouse_id,destination_warehouse_id,priority,enabled,version,updated_at`, s.OrganizationID(), s.WorkspaceID(), route.SourceWarehouseID.String(), route.DestinationWarehouseID.String(), route.Priority, route.Enabled, expectedVersion).Scan(&out.SourceWarehouseID, &out.DestinationWarehouseID, &out.Priority, &out.Enabled, &out.Version, &out.UpdatedAt)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return inventory.FailoverRoute{}, inventory.ErrConflict
	}
	out.UpdatedAt = out.UpdatedAt.UTC()
	return out, err
}

func (r *Repository) ResolveFailover(ctx context.Context, s inventory.Scope, source inventory.WarehouseID, offer inventory.OfferID) (inventory.FailoverDecision, error) {
	if ctx == nil || r == nil || !s.Valid() || !source.Valid() || !offer.Valid() {
		return inventory.FailoverDecision{}, inventory.ErrInvalidRecord
	}
	decision := inventory.FailoverDecision{SourceWarehouseID: source, OfferID: offer, OccurredAt: time.Now().UTC()}
	decision.ID = randomFailoverID()
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		var err error
		decision, err = resolveFailoverTx(ctx, tx, s, source, offer, decision.ID, decision.OccurredAt, true)
		return err
	})
	return decision, err
}

func resolveFailoverTx(ctx context.Context, tx *sql.Tx, s inventory.Scope, source inventory.WarehouseID, offer inventory.OfferID, decisionID string, at time.Time, persistLegacy bool) (inventory.FailoverDecision, error) {
	decision := inventory.FailoverDecision{ID: decisionID, SourceWarehouseID: source, OfferID: offer, OccurredAt: at.UTC()}
	var sourceState string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT state FROM warehouse_operational_state WHERE organization_id=$1 AND workspace_id=$2 AND warehouse_id=$3),'active')`, s.OrganizationID(), s.WorkspaceID(), source.String()).Scan(&sourceState); err != nil {
		return inventory.FailoverDecision{}, err
	}
	if sourceState != "unavailable" && sourceState != "lost" {
		return inventory.FailoverDecision{}, inventory.ErrWarehouseInactive
	}
	var destination string
	err := tx.QueryRowContext(ctx, `SELECT r.destination_warehouse_id FROM warehouse_failover_routes r JOIN warehouses w ON w.organization_id=r.organization_id AND w.workspace_id=r.workspace_id AND w.id=r.destination_warehouse_id LEFT JOIN warehouse_operational_state st ON st.organization_id=r.organization_id AND st.workspace_id=r.workspace_id AND st.warehouse_id=r.destination_warehouse_id JOIN inventory_positions p ON p.organization_id=r.organization_id AND p.workspace_id=r.workspace_id AND p.warehouse_id=r.destination_warehouse_id AND p.offer_id=$4 WHERE r.organization_id=$1 AND r.workspace_id=$2 AND r.source_warehouse_id=$3 AND r.enabled AND w.status='active' AND COALESCE(st.state,'active') IN ('active','degraded') AND (p.on_hand_coefficient::numeric / power(10::numeric,p.on_hand_scale) - p.reserved_coefficient::numeric / power(10::numeric,p.reserved_scale)) > 0 ORDER BY CASE COALESCE(st.state,'active') WHEN 'active' THEN 0 ELSE 1 END,r.priority,r.destination_warehouse_id LIMIT 1`, s.OrganizationID(), s.WorkspaceID(), source.String(), offer.String()).Scan(&destination)
	result := "routed"
	var destinationValue any
	if errors.Is(err, sql.ErrNoRows) {
		result = "no_eligible_destination"
		destination = ""
	} else if err != nil {
		return inventory.FailoverDecision{}, err
	} else {
		decision.Routed = true
		decision.DestinationWarehouseID = inventory.WarehouseID(destination)
		destinationValue = destination
	}
	if persistLegacy {
		if _, err = tx.ExecContext(ctx, `INSERT INTO warehouse_failover_decisions(organization_id,workspace_id,decision_id,source_warehouse_id,destination_warehouse_id,offer_id,result,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, s.OrganizationID(), s.WorkspaceID(), decision.ID, source.String(), destinationValue, offer.String(), result, decision.OccurredAt); err != nil {
			return inventory.FailoverDecision{}, err
		}
	}
	return decision, nil
}

func (r *Repository) WarehouseIncident(ctx context.Context, s inventory.Scope, incidentID string) (inventory.WarehouseIncident, error) {
	if ctx == nil || r == nil || !s.Valid() || !validIncidentID(incidentID) {
		return inventory.WarehouseIncident{}, inventory.ErrInvalidRecord
	}
	var out inventory.WarehouseIncident
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		return scanWarehouseIncident(tx.QueryRowContext(ctx, `SELECT incident_id,warehouse_id,operational_state,COALESCE(reason_code,''),status,COALESCE(cursor_offer_id,''),routed_count,no_route_count,rerouted_allocation_count,execution_attention_count,opened_at,updated_at,completed_at FROM warehouse_incidents WHERE organization_id=$1 AND workspace_id=$2 AND incident_id=$3`, s.OrganizationID(), s.WorkspaceID(), incidentID), &out)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return inventory.WarehouseIncident{}, inventory.ErrNotFound
	}
	return out, err
}

func (r *Repository) ListWarehouseIncidents(ctx context.Context, s inventory.Scope, limit int) ([]inventory.WarehouseIncident, error) {
	if ctx == nil || r == nil || !s.Valid() || limit < 1 || limit > 200 {
		return nil, inventory.ErrInvalidRecord
	}
	out := make([]inventory.WarehouseIncident, 0, limit)
	err := r.withTx(ctx, true, s, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT incident_id,warehouse_id,operational_state,COALESCE(reason_code,''),status,COALESCE(cursor_offer_id,''),routed_count,no_route_count,rerouted_allocation_count,execution_attention_count,opened_at,updated_at,completed_at FROM warehouse_incidents WHERE organization_id=$1 AND workspace_id=$2 ORDER BY opened_at DESC,incident_id DESC LIMIT $3`, s.OrganizationID(), s.WorkspaceID(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item inventory.WarehouseIncident
			if err := scanWarehouseIncident(rows, &item); err != nil {
				return err
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}

func (r *Repository) ProcessWarehouseIncidentBatch(ctx context.Context, s inventory.Scope, incidentID string, limit int) (inventory.WarehouseIncident, error) {
	if ctx == nil || r == nil || !s.Valid() || !validIncidentID(incidentID) || limit < 1 || limit > 500 {
		return inventory.WarehouseIncident{}, inventory.ErrInvalidRecord
	}
	var out inventory.WarehouseIncident
	err := r.withTx(ctx, false, s, func(tx *sql.Tx) error {
		if err := scanWarehouseIncident(tx.QueryRowContext(ctx, `SELECT incident_id,warehouse_id,operational_state,COALESCE(reason_code,''),status,COALESCE(cursor_offer_id,''),routed_count,no_route_count,rerouted_allocation_count,execution_attention_count,opened_at,updated_at,completed_at FROM warehouse_incidents WHERE organization_id=$1 AND workspace_id=$2 AND incident_id=$3 FOR UPDATE`, s.OrganizationID(), s.WorkspaceID(), incidentID), &out); err != nil {
			return err
		}
		if out.Status != inventory.WarehouseIncidentOpen && out.Status != inventory.WarehouseIncidentProcessing {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE warehouse_incidents SET status='processing',updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND incident_id=$3`, s.OrganizationID(), s.WorkspaceID(), incidentID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT offer_id FROM inventory_positions WHERE organization_id=$1 AND workspace_id=$2 AND warehouse_id=$3 AND offer_id>$4 AND (on_hand_coefficient<>0 OR reserved_coefficient<>0) ORDER BY offer_id LIMIT $5`, s.OrganizationID(), s.WorkspaceID(), out.WarehouseID.String(), out.CursorOfferID.String(), limit)
		if err != nil {
			return err
		}
		offers := make([]inventory.OfferID, 0, limit)
		for rows.Next() {
			var offer inventory.OfferID
			if err := rows.Scan(&offer); err != nil {
				rows.Close()
				return err
			}
			offers = append(offers, offer)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, offer := range offers {
			execution, err := executeOfferFailoverTx(ctx, tx, s, incidentID, out.WarehouseID, offer, time.Now().UTC())
			if err != nil {
				return err
			}
			decision := execution.Decision
			result := "no_eligible_destination"
			var destination any
			if decision.Routed {
				result = "routed"
				destination = decision.DestinationWarehouseID.String()
			}
			res, err := tx.ExecContext(ctx, `INSERT INTO warehouse_incident_decisions(organization_id,workspace_id,incident_id,offer_id,destination_warehouse_id,result,execution_status,execution_reason,rerouted_allocations,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`, s.OrganizationID(), s.WorkspaceID(), incidentID, offer.String(), destination, result, execution.ExecutionStatus, execution.ExecutionReason, execution.ReroutedAllocations, decision.OccurredAt)
			if err != nil {
				return err
			}
			if changed, _ := res.RowsAffected(); changed == 1 {
				if decision.Routed {
					out.RoutedCount++
				} else {
					out.NoRouteCount++
				}
				out.ReroutedAllocationCount += execution.ReroutedAllocations
				out.ExecutionAttentionCount += execution.ExecutionAttention
			}
			out.CursorOfferID = offer
		}
		var remaining bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_positions WHERE organization_id=$1 AND workspace_id=$2 AND warehouse_id=$3 AND offer_id>$4 AND (on_hand_coefficient<>0 OR reserved_coefficient<>0))`, s.OrganizationID(), s.WorkspaceID(), out.WarehouseID.String(), out.CursorOfferID.String()).Scan(&remaining); err != nil {
			return err
		}
		status := inventory.WarehouseIncidentProcessing
		var completed any
		if !remaining {
			status = inventory.WarehouseIncidentCompleted
			if out.NoRouteCount > 0 || out.ExecutionAttentionCount > 0 {
				status = inventory.WarehouseIncidentNeedsAttention
			}
			completed = time.Now().UTC()
		}
		var cursor any
		if out.CursorOfferID.Valid() {
			cursor = out.CursorOfferID.String()
		}
		return scanWarehouseIncident(tx.QueryRowContext(ctx, `UPDATE warehouse_incidents SET status=$4,cursor_offer_id=$5,routed_count=$6,no_route_count=$7,rerouted_allocation_count=$8,execution_attention_count=$9,updated_at=clock_timestamp(),completed_at=$10 WHERE organization_id=$1 AND workspace_id=$2 AND incident_id=$3 RETURNING incident_id,warehouse_id,operational_state,COALESCE(reason_code,''),status,COALESCE(cursor_offer_id,''),routed_count,no_route_count,rerouted_allocation_count,execution_attention_count,opened_at,updated_at,completed_at`, s.OrganizationID(), s.WorkspaceID(), incidentID, string(status), cursor, out.RoutedCount, out.NoRouteCount, out.ReroutedAllocationCount, out.ExecutionAttentionCount, completed), &out)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return inventory.WarehouseIncident{}, inventory.ErrNotFound
	}
	return out, err
}

type rowScanner interface{ Scan(...any) error }

func scanWarehouseIncident(row rowScanner, out *inventory.WarehouseIncident) error {
	if out == nil {
		return errors.New("inventory repository: incident target required")
	}
	var completed sql.NullTime
	if err := row.Scan(&out.ID, &out.WarehouseID, &out.OperationalState, &out.ReasonCode, &out.Status, &out.CursorOfferID, &out.RoutedCount, &out.NoRouteCount, &out.ReroutedAllocationCount, &out.ExecutionAttentionCount, &out.OpenedAt, &out.UpdatedAt, &completed); err != nil {
		return err
	}
	out.OpenedAt = out.OpenedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	if completed.Valid {
		at := completed.Time.UTC()
		out.CompletedAt = &at
	} else {
		out.CompletedAt = nil
	}
	if !out.WarehouseID.Valid() || !out.OperationalState.Valid() || !out.Status.Valid() || out.ID == "" || out.RoutedCount < 0 || out.NoRouteCount < 0 {
		return errors.New("inventory repository: invalid persisted warehouse incident")
	}
	return nil
}

func validIncidentID(value string) bool {
	if len(value) != len("whinc_")+32 || value[:len("whinc_")] != "whinc_" {
		return false
	}
	_, err := hex.DecodeString(value[len("whinc_"):])
	return err == nil
}

func randomIncidentID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("inventory repository: incident id: %w", err)
	}
	return "whinc_" + hex.EncodeToString(b), nil
}

func randomFailoverID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "failover_" + hex.EncodeToString(b)
}
