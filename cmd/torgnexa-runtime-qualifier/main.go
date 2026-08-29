// Command torgnexa-runtime-qualifier performs black-box runtime qualification
// from inside the deployed application image. It intentionally uses the same
// PostgreSQL and Kafka configuration as the worker and fails closed on timeout.
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	coreinventory "github.com/torgnexa/torgnexa/internal/core/inventory"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/config"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/kafkaeventbus"
	"github.com/torgnexa/torgnexa/internal/platform/kafkatransport"
	"github.com/torgnexa/torgnexa/internal/platform/outbox"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/database"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inventoryrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
)

const (
	qualOrganization = "0198b8d0-0000-7000-8000-00000000f201"
	qualWorkspace    = "0198b8d0-0000-7000-8000-00000000f202"
	qualConsumerWait = 30 * time.Second
	qualProduct      = "0198b8d0-0000-7000-8000-00000000f211"
	qualOffer        = "0198b8d0-0000-7000-8000-00000000f212"
	qualWarehouseA   = "0198b8d0-0000-7000-8000-00000000f213"
	qualWarehouseB   = "0198b8d0-0000-7000-8000-00000000f214"
	qualPositionA    = "0198b8d0-0000-7000-8000-00000000f215"
	qualPositionB    = "0198b8d0-0000-7000-8000-00000000f216"
)

type result struct {
	Status                           string `json:"status"`
	OutboxPublishMillis              int64  `json:"outbox_publish_ms"`
	InboxProcessMillis               int64  `json:"inbox_process_ms"`
	DuplicateReceiptCount            int    `json:"duplicate_receipt_count"`
	MarkerReceiptObserved            bool   `json:"marker_receipt_observed"`
	WarehouseIncidentObserved        bool   `json:"warehouse_incident_observed"`
	WarehouseRoutedCount             int    `json:"warehouse_routed_count"`
	WarehouseReroutedAllocationCount int    `json:"warehouse_rerouted_allocation_count"`
	WarehouseExecutionAttentionCount int    `json:"warehouse_execution_attention_count"`
	WarehouseSourceUnchanged         bool   `json:"warehouse_source_stock_unchanged"`
	WarehouseDestinationValid        bool   `json:"warehouse_destination_valid"`
	WarehouseAllocationRerouted      bool   `json:"warehouse_allocation_rerouted"`
	WarehouseSourceReservationFreed  bool   `json:"warehouse_source_reservation_released"`
	WarehouseDestinationReserved     bool   `json:"warehouse_destination_reserved"`
	FulfillmentOutboxObserved        bool   `json:"fulfillment_outbox_observed"`
	WorkerConsumer                   string `json:"worker_consumer"`
	QualifiedAt                      string `json:"qualified_at"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg, err := config.Load(config.ServiceWorker)
	if err != nil {
		fatal("configuration", err)
	}
	db, err := database.Open(ctx, cfg.Database)
	if err != nil {
		fatal("database", err)
	}
	defer db.Close()
	scope, err := tenancy.ParseScope(qualOrganization, qualWorkspace)
	if err != nil {
		fatal("scope", err)
	}
	if err := seedTenant(ctx, db, scope); err != nil {
		fatal("tenant_seed", err)
	}

	now := time.Now().UTC()
	first := makeEvent(now, "p3e2e-"+formatIDTime(now), "probe-primary")
	started := time.Now()
	if err := enqueue(ctx, db, scope, first); err != nil {
		fatal("outbox_enqueue", err)
	}
	publishedAt, err := waitPublished(ctx, db, scope, first.ID, qualConsumerWait)
	if err != nil {
		fatal("outbox_publish", err)
	}
	processedAt, err := waitReceipt(ctx, db, scope, cfg.Worker.KafkaConsumerGroup, first.ID, qualConsumerWait)
	if err != nil {
		fatal("inbox_process", err)
	}

	producer, err := kafkatransport.NewProducer(cfg.Worker.KafkaBrokers, "torgnexa-runtime-qualifier")
	if err != nil {
		fatal("kafka_producer", err)
	}
	defer producer.Close()
	publisher, err := kafkaeventbus.NewPublisher(producer)
	if err != nil {
		fatal("event_publisher", err)
	}
	// Deliver the same immutable event again, then a marker. Seeing the marker
	// while retaining exactly one first receipt proves duplicate delivery did not
	// poison the consumer and the Inbox idempotency boundary stayed effective.
	if err := publisher.Publish(ctx, first); err != nil {
		fatal("duplicate_publish", err)
	}
	markerAt := time.Now().UTC()
	marker := makeEvent(markerAt, "p3e2e-"+formatIDTime(markerAt)+"-marker", "probe-marker")
	if err := publisher.Publish(ctx, marker); err != nil {
		fatal("marker_publish", err)
	}
	if _, err := waitReceipt(ctx, db, scope, cfg.Worker.KafkaConsumerGroup, marker.ID, qualConsumerWait); err != nil {
		fatal("marker_receipt", err)
	}
	count, err := receiptCount(ctx, db, scope, cfg.Worker.KafkaConsumerGroup, first.ID)
	if err != nil {
		fatal("duplicate_receipt_count", err)
	}
	if count != 1 {
		fatal("inbox_idempotency", fmt.Errorf("expected one receipt, got %d", count))
	}

	incident, sourceUnchanged, destinationValid, allocationRerouted, sourceReservationFreed, destinationReserved, fulfillmentOutbox, err := qualifyWarehouseIncident(ctx, db, scope)
	if err != nil {
		fatal("warehouse_incident", err)
	}
	if incident.Status != coreinventory.WarehouseIncidentCompleted || incident.RoutedCount < 1 || incident.NoRouteCount != 0 || incident.ReroutedAllocationCount < 1 || incident.ExecutionAttentionCount != 0 || !sourceUnchanged || !destinationValid || !allocationRerouted || !sourceReservationFreed || !destinationReserved || !fulfillmentOutbox {
		fatal("warehouse_incident_invariant", fmt.Errorf("status=%s routed=%d no_route=%d rerouted_allocations=%d attention=%d source_unchanged=%t destination_valid=%t allocation_rerouted=%t source_reservation_released=%t destination_reserved=%t fulfillment_outbox=%t", incident.Status, incident.RoutedCount, incident.NoRouteCount, incident.ReroutedAllocationCount, incident.ExecutionAttentionCount, sourceUnchanged, destinationValid, allocationRerouted, sourceReservationFreed, destinationReserved, fulfillmentOutbox))
	}

	out := result{
		Status: "PASS", OutboxPublishMillis: publishedAt.Sub(started).Milliseconds(),
		InboxProcessMillis: processedAt.Sub(started).Milliseconds(), DuplicateReceiptCount: count,
		MarkerReceiptObserved: true, WarehouseIncidentObserved: true, WarehouseRoutedCount: incident.RoutedCount,
		WarehouseReroutedAllocationCount: incident.ReroutedAllocationCount, WarehouseExecutionAttentionCount: incident.ExecutionAttentionCount,
		WarehouseSourceUnchanged: sourceUnchanged, WarehouseDestinationValid: destinationValid, WarehouseAllocationRerouted: allocationRerouted,
		WarehouseSourceReservationFreed: sourceReservationFreed, WarehouseDestinationReserved: destinationReserved, FulfillmentOutboxObserved: fulfillmentOutbox, WorkerConsumer: cfg.Worker.KafkaConsumerGroup,
		QualifiedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fatal("output", err)
	}
}

func seedTenant(ctx context.Context, db *sql.DB, scope tenancy.Scope) error {
	return withScope(ctx, db, scope, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO organizations(id,name,status,version,created_at,updated_at) VALUES($1,'P3 qualification','active',1,clock_timestamp(),clock_timestamp()) ON CONFLICT(id) DO NOTHING`, scope.OrganizationID().String()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO workspaces(id,organization_id,name,status,version,created_at,updated_at) VALUES($1,$2,'P3 qualification','active',1,clock_timestamp(),clock_timestamp()) ON CONFLICT(id) DO NOTHING`, scope.WorkspaceID().String(), scope.OrganizationID().String())
		return err
	})
}

func qualifyWarehouseIncident(ctx context.Context, db *sql.DB, scope tenancy.Scope) (coreinventory.WarehouseIncident, bool, bool, bool, bool, bool, bool, error) {
	if err := seedWarehouseFixture(ctx, db, scope, false); err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	inventoryScope, err := coreinventory.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	repository, err := inventoryrepo.New(db)
	if err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	source := coreinventory.WarehouseID(qualWarehouseA)
	if _, err := restoreQualificationSource(ctx, repository, inventoryScope, source); err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	if err := seedWarehouseFixture(ctx, db, scope, true); err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	orderID, orderItemID, allocationID, err := seedQualificationOrder(ctx, db, scope)
	if err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	beforeSourceOnHand, beforeSourceReserved, err := positionBalances(ctx, db, scope, qualPositionA)
	if err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	_, beforeDestinationReserved, err := positionBalances(ctx, db, scope, qualPositionB)
	if err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	at := time.Now().UTC()
	mutation := coreinventory.Mutation{EventID: "p3-reserve-" + formatIDTime(at), AuditID: mustQualifierUUID(), ActorID: "runtime-qualifier", Source: "qualification", CorrelationID: "p3-reserve-" + formatIDTime(at), OccurredAt: at}
	allocation, err := repository.ReserveOrderItem(ctx, inventoryScope, coreinventory.ReserveOrderItem{AllocationID: allocationID, OrderItemID: orderItemID, IdempotencyKey: "p3qual-" + formatIDTime(at), WarehouseID: source}, mutation)
	if err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, fmt.Errorf("reserve qualification order item: %w", err)
	}
	if allocation.OrderID != orderID || allocation.Status != coreinventory.FulfillmentReserved {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, errors.New("source fulfillment allocation not reserved")
	}
	_, sourceReservedAfterReserve, err := positionBalances(ctx, db, scope, qualPositionA)
	if err != nil || sourceReservedAfterReserve <= beforeSourceReserved {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, errors.New("source reservation did not increase")
	}
	if err := markQualificationSourceLost(ctx, repository, inventoryScope, source); err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	incidents, err := repository.ListWarehouseIncidents(ctx, inventoryScope, 20)
	if err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	var incident coreinventory.WarehouseIncident
	for _, candidate := range incidents {
		if candidate.WarehouseID == source && (candidate.Status == coreinventory.WarehouseIncidentOpen || candidate.Status == coreinventory.WarehouseIncidentProcessing) {
			incident = candidate
			break
		}
	}
	if incident.ID == "" {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, errors.New("warehouse incident was not opened")
	}
	deadline := time.Now().Add(qualConsumerWait)
	for time.Now().Before(deadline) {
		incident, err = repository.WarehouseIncident(ctx, inventoryScope, incident.ID)
		if err != nil {
			return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
		}
		if incident.Status == coreinventory.WarehouseIncidentCompleted || incident.Status == coreinventory.WarehouseIncidentNeedsAttention || incident.Status == coreinventory.WarehouseIncidentResolved {
			break
		}
		if err := sleep(ctx, 100*time.Millisecond); err != nil {
			return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
		}
	}
	afterSourceOnHand, afterSourceReserved, err := positionBalances(ctx, db, scope, qualPositionA)
	if err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	_, afterDestinationReserved, err := positionBalances(ctx, db, scope, qualPositionB)
	if err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	var destination string
	var executionStatus string
	err = queryScope(ctx, db, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT destination_warehouse_id,execution_status FROM warehouse_incident_decisions WHERE organization_id=$1 AND workspace_id=$2 AND incident_id=$3 AND offer_id=$4 AND result='routed'`, scope.OrganizationID().String(), scope.WorkspaceID().String(), incident.ID, qualOffer).Scan(&destination, &executionStatus)
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	var replacementCount int
	err = queryScope(ctx, db, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM fulfillment_allocations WHERE organization_id=$1 AND workspace_id=$2 AND replaces_allocation_id=$3 AND incident_id=$4 AND warehouse_id=$5 AND state='reserved'`, scope.OrganizationID().String(), scope.WorkspaceID().String(), allocationID, incident.ID, qualWarehouseB).Scan(&replacementCount)
	})
	if err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	var sourceState string
	if err := queryScope(ctx, db, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT state FROM fulfillment_allocations WHERE organization_id=$1 AND workspace_id=$2 AND allocation_id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), allocationID).Scan(&sourceState)
	}); err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	var outboxCount int
	if err := queryScope(ctx, db, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM outbox_events WHERE organization_id=$1 AND workspace_id=$2 AND event_type='commerce.fulfillment.allocation_changed.v1' AND created_at >= $3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), at.Add(-time.Second)).Scan(&outboxCount)
	}); err != nil {
		return coreinventory.WarehouseIncident{}, false, false, false, false, false, false, err
	}
	return incident,
		beforeSourceOnHand == afterSourceOnHand,
		destination == qualWarehouseB,
		executionStatus == "rerouted" && sourceState == "released" && replacementCount == 1,
		afterSourceReserved == beforeSourceReserved,
		afterDestinationReserved > beforeDestinationReserved,
		outboxCount >= 2,
		nil
}

func restoreQualificationSource(ctx context.Context, repository *inventoryrepo.Repository, scope coreinventory.Scope, source coreinventory.WarehouseID) (coreinventory.WarehouseOperationalState, error) {
	var last coreinventory.WarehouseOperationalState
	for attempt := 0; attempt < 5; attempt++ {
		state, err := repository.OperationalState(ctx, scope, source)
		if err != nil {
			return coreinventory.WarehouseOperationalState{}, fmt.Errorf("read source operational state: %w", err)
		}
		last = state
		if state.State == coreinventory.OperationalActive {
			return state, nil
		}
		state, err = repository.SetOperationalState(ctx, scope, source, coreinventory.OperationalActive, "", state.Version)
		if err == nil {
			return state, nil
		}
		if !errors.Is(err, coreinventory.ErrConflict) || attempt == 4 {
			return coreinventory.WarehouseOperationalState{}, fmt.Errorf("restore source operational state: %w (observed state=%s version=%d)", err, last.State, last.Version)
		}
		if err := sleep(ctx, 50*time.Millisecond); err != nil {
			return coreinventory.WarehouseOperationalState{}, fmt.Errorf("restore source operational state retry: %w", err)
		}
	}
	return coreinventory.WarehouseOperationalState{}, errors.New("restore source operational state: retry budget exhausted")
}

func markQualificationSourceLost(ctx context.Context, repository *inventoryrepo.Repository, scope coreinventory.Scope, source coreinventory.WarehouseID) error {
	for attempt := 0; attempt < 5; attempt++ {
		state, err := repository.OperationalState(ctx, scope, source)
		if err != nil {
			return fmt.Errorf("read source state before loss: %w", err)
		}
		_, err = repository.SetOperationalState(ctx, scope, source, coreinventory.OperationalLost, "qualification_loss", state.Version)
		if err == nil {
			return nil
		}
		if !errors.Is(err, coreinventory.ErrConflict) || attempt == 4 {
			return fmt.Errorf("mark source operational lost: %w", err)
		}
		if err := sleep(ctx, 50*time.Millisecond); err != nil {
			return fmt.Errorf("mark source operational lost retry: %w", err)
		}
	}
	return errors.New("mark source operational lost: retry budget exhausted")
}

func seedWarehouseFixture(ctx context.Context, db *sql.DB, scope tenancy.Scope, includePositions bool) error {
	return withScope(ctx, db, scope, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		statements := []struct {
			query string
			args  []any
		}{
			{`INSERT INTO products(id,organization_id,workspace_id,code,title,description,status,version,created_at,updated_at) VALUES($1,$2,$3,'P2QUAL','P3 qualification product','','draft',1,clock_timestamp(),clock_timestamp()) ON CONFLICT(id) DO NOTHING`, []any{qualProduct, org, workspace}},
			{`INSERT INTO offers(id,organization_id,workspace_id,product_id,sku,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,'P2QUAL-SKU','draft',1,clock_timestamp(),clock_timestamp()) ON CONFLICT(id) DO NOTHING`, []any{qualOffer, org, workspace, qualProduct}},
			{`INSERT INTO warehouses(id,organization_id,workspace_id,code,name,status,version,created_at,updated_at) VALUES($1,$2,$3,'P2-WH-A','P3 source warehouse','active',1,clock_timestamp(),clock_timestamp()) ON CONFLICT(id) DO NOTHING`, []any{qualWarehouseA, org, workspace}},
			{`INSERT INTO warehouses(id,organization_id,workspace_id,code,name,status,version,created_at,updated_at) VALUES($1,$2,$3,'P2-WH-B','P3 backup warehouse','active',1,clock_timestamp(),clock_timestamp()) ON CONFLICT(id) DO NOTHING`, []any{qualWarehouseB, org, workspace}},
			{`INSERT INTO warehouse_failover_routes(organization_id,workspace_id,source_warehouse_id,destination_warehouse_id,priority,enabled,version,updated_at) VALUES($1,$2,$3,$4,1,true,1,clock_timestamp()) ON CONFLICT(organization_id,workspace_id,source_warehouse_id,destination_warehouse_id) DO NOTHING`, []any{org, workspace, qualWarehouseA, qualWarehouseB}},
		}
		for _, statement := range statements {
			if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
				return err
			}
		}
		// The commerce lifecycle requires new masters to start as draft@v1;
		// promote the synthetic fixture through the same guarded transition
		// before using its offer in the qualification order.
		if _, err := tx.ExecContext(ctx, `UPDATE products SET status='active',version=2,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND status='draft' AND version=1`, org, workspace, qualProduct); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE offers SET status='active',version=2,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND status='draft' AND version=1`, org, workspace, qualOffer); err != nil {
			return err
		}
		if !includePositions {
			return nil
		}
		for _, fixture := range []struct {
			id, warehouse string
		}{
			{qualPositionA, qualWarehouseA},
			{qualPositionB, qualWarehouseB},
		} {
			result, err := tx.ExecContext(ctx, `INSERT INTO inventory_positions(id,organization_id,workspace_id,offer_id,warehouse_id,on_hand_coefficient,on_hand_scale,reserved_coefficient,reserved_scale,unit,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,0,0,0,0,'PCS',1,clock_timestamp(),clock_timestamp()) ON CONFLICT(id) DO NOTHING`, fixture.id, org, workspace, qualOffer, fixture.warehouse)
			if err != nil {
				return err
			}
			inserted, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if inserted == 1 {
				if _, err := tx.ExecContext(ctx, `UPDATE inventory_positions SET on_hand_coefficient=1000,on_hand_scale=0,version=2,updated_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, org, workspace, fixture.id); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func positionBalances(ctx context.Context, db *sql.DB, scope tenancy.Scope, positionID string) (int64, int64, error) {
	var onHand, reserved int64
	err := queryScope(ctx, db, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT on_hand_coefficient,reserved_coefficient FROM inventory_positions WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND on_hand_scale=0 AND reserved_scale=0`, scope.OrganizationID().String(), scope.WorkspaceID().String(), positionID).Scan(&onHand, &reserved)
	})
	return onHand, reserved, err
}

func seedQualificationOrder(ctx context.Context, db *sql.DB, scope tenancy.Scope) (string, string, string, error) {
	orderID, orderItemID, allocationID := mustQualifierUUID(), mustQualifierUUID(), mustQualifierUUID()
	err := withScope(ctx, db, scope, func(tx *sql.Tx) error {
		org, workspace := scope.OrganizationID().String(), scope.WorkspaceID().String()
		if _, err := tx.ExecContext(ctx, `INSERT INTO orders(id,organization_id,workspace_id,order_number,status,currency,subtotal_minor_units,discount_minor_units,tax_minor_units,shipping_minor_units,grand_minor_units,placed_at,version,created_at,updated_at) VALUES($1,$2,$3,$4,'pending','RUB',300,0,0,0,300,clock_timestamp(),1,clock_timestamp(),clock_timestamp())`, orderID, org, workspace, "P3QUAL-"+formatIDTime(time.Now().UTC())); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO order_items(id,organization_id,workspace_id,order_id,position,offer_id,sku_snapshot,quantity_coefficient,quantity_scale,unit,unit_price_minor_units,subtotal_minor_units,discount_minor_units,tax_minor_units,line_total_minor_units,tax_jurisdiction,tax_category,tax_rate_coefficient,tax_rate_scale,price_includes_tax,created_at) VALUES($1,$2,$3,$4,1,$5,'P2QUAL-SKU',3,0,'PCS',100,300,0,0,300,'RU','zero',0,0,true,clock_timestamp())`, orderItemID, org, workspace, orderID, qualOffer)
		return err
	})
	return orderID, orderItemID, allocationID, err
}

func mustQualifierUUID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	millis := uint64(time.Now().UTC().UnixMilli())
	value[0], value[1], value[2], value[3], value[4], value[5] = byte(millis>>40), byte(millis>>32), byte(millis>>24), byte(millis>>16), byte(millis>>8), byte(millis)
	value[6], value[8] = (value[6]&0x0f)|0x70, (value[8]&0x3f)|0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

func enqueue(ctx context.Context, db *sql.DB, scope tenancy.Scope, event eventbus.Event) error {
	return withScope(ctx, db, scope, func(tx *sql.Tx) error {
		enqueuer, err := outboxrepo.NewTransactionEnqueuer(tx)
		if err != nil {
			return err
		}
		return enqueuer.Enqueue(ctx, event)
	})
}

func makeEvent(at time.Time, id, entityID string) eventbus.Event {
	instant, _ := domain.NewUTCInstant(at)
	typeValue, _ := eventbus.ParseEventType("commerce.inventory.stock_changed.v1")
	return eventbus.Event{
		ID: id, Type: typeValue, OccurredAt: instant, OrganizationID: qualOrganization,
		WorkspaceID: qualWorkspace, EntityType: "qualification_probe", EntityID: entityID,
		Source: "qualification", CorrelationID: id, Data: json.RawMessage(`{"probe":"p3-runtime"}`),
	}
}

func formatIDTime(value time.Time) string { return fmt.Sprintf("%d", value.UnixNano()) }

func waitPublished(ctx context.Context, db *sql.DB, scope tenancy.Scope, eventID string, timeout time.Duration) (time.Time, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var published sql.NullTime
		err := queryScope(ctx, db, scope, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT published_at FROM outbox_events WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), eventID).Scan(&published)
		})
		if err == nil && published.Valid {
			return published.Time.UTC(), nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, err
		}
		if err := sleep(ctx, 100*time.Millisecond); err != nil {
			return time.Time{}, err
		}
	}
	return time.Time{}, outbox.ErrLeaseLost
}

func waitReceipt(ctx context.Context, db *sql.DB, scope tenancy.Scope, consumer, eventID string, timeout time.Duration) (time.Time, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var processed time.Time
		err := queryScope(ctx, db, scope, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT processed_at FROM inbox_receipts WHERE organization_id=$1 AND workspace_id=$2 AND consumer=$3 AND event_id=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), consumer, eventID).Scan(&processed)
		})
		if err == nil {
			return processed.UTC(), nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, err
		}
		if err := sleep(ctx, 100*time.Millisecond); err != nil {
			return time.Time{}, err
		}
	}
	return time.Time{}, errors.New("receipt timeout")
}

func receiptCount(ctx context.Context, db *sql.DB, scope tenancy.Scope, consumer, eventID string) (int, error) {
	var count int
	err := queryScope(ctx, db, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM inbox_receipts WHERE organization_id=$1 AND workspace_id=$2 AND consumer=$3 AND event_id=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), consumer, eventID).Scan(&count)
	})
	return count, err
}

func withScope(ctx context.Context, db *sql.DB, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var organizationID, workspaceID string
	if err := tx.QueryRowContext(ctx, `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&organizationID, &workspaceID); err != nil {
		return err
	}
	if organizationID != scope.OrganizationID().String() || workspaceID != scope.WorkspaceID().String() {
		return errors.New("tenant scope mismatch")
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func queryScope(ctx context.Context, db *sql.DB, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	return withScope(ctx, db, scope, fn)
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func fatal(stage string, err error) {
	_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"status": "FAIL", "stage": stage, "error": err.Error()})
	os.Exit(1)
}
