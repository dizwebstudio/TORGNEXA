// Package logisticsrepo persists the tenant-scoped shipment lifecycle.
//
// This repository deliberately stops at the local durable boundary. A caller
// may claim an idempotency key and enqueue a worker command, but no method in
// this package performs a carrier HTTP request.
package logisticsrepo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/logistics"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
)

const (
	applyScopeStatement = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`
	createOperation     = "fulfillment.shipment.create"
	cancelOperation     = "fulfillment.shipment.cancel"
	shipmentSelect      = `shipment_id,organization_id,workspace_id,provider_account_id,external_id,remote_id,service_code,status,tracking_number,cost_minor_units,currency,min_delivery_at,max_delivery_at,version,updated_at`
)

// Repository stores canonical shipment projections and operation receipts.
type Repository struct{ database *sql.DB }

var _ logistics.Repository = (*Repository)(nil)

// New constructs a tenant-scoped shipment repository.
func New(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, errors.New("logistics repository: database is required")
	}
	return &Repository{database: database}, nil
}

// Shipment returns a shipment or ErrNotFound. Missing and out-of-scope rows
// intentionally have the same result.
func (repository *Repository) Shipment(ctx context.Context, scope tenancy.Scope, id logistics.ShipmentID) (logistics.Shipment, error) {
	if err := validate(ctx, repository, scope); err != nil {
		return logistics.Shipment{}, err
	}
	if !id.Valid() {
		return logistics.Shipment{}, logistics.ErrInvalidRecord
	}
	var result logistics.Shipment
	err := repository.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		result, err = loadShipment(ctx, tx, scope, id, false)
		return err
	})
	return result, err
}

// BeginCreate claims a shipment creation command. fresh is true only for the
// caller that owns the subsequent remote side effect.
func (repository *Repository) BeginCreate(ctx context.Context, scope tenancy.Scope, command logistics.CreateCommand, mutation logistics.Mutation) (logistics.Shipment, bool, error) {
	if err := validate(ctx, repository, scope); err != nil {
		return logistics.Shipment{}, false, err
	}
	if err := command.Validate(); err != nil {
		return logistics.Shipment{}, false, err
	}
	if err := mutation.Validate(); err != nil {
		return logistics.Shipment{}, false, err
	}
	var result logistics.Shipment
	fresh := false
	err := repository.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		digest := createDigest(command)
		resourceID, claimed, err := claimReceipt(ctx, tx, scope, createOperation, command.IdempotencyKey, digest)
		if err != nil {
			if errors.Is(err, logistics.ErrConflict) {
				return err
			}
			return fmt.Errorf("claim shipment creation: %w", err)
		}
		if !claimed {
			result, err = loadShipment(ctx, tx, scope, logistics.ShipmentID(resourceID), false)
			return err
		}

		fresh = true
		at := mutation.OccurredAt.UTC()
		_, err = tx.ExecContext(ctx, `INSERT INTO logistics_shipments(organization_id,workspace_id,shipment_id,provider_account_id,external_id,remote_id,service_code,status,tracking_number,cost_minor_units,currency,version,updated_at) VALUES($1,$2,$3,$4,$5,'',$6,$7,'',0,'RUB',1,$8)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), command.ID.String(), command.AccountID, command.ExternalID, command.ServiceCode, logistics.StatusPending, at)
		if err != nil {
			return fmt.Errorf("insert pending shipment: %w", err)
		}
		if err := bindReceiptResource(ctx, tx, scope, createOperation, command.IdempotencyKey, command.ID.String()); err != nil {
			return err
		}
		result, err = loadShipment(ctx, tx, scope, command.ID, false)
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "fulfillment.shipment.create.requested", command.ID.String(), audit.RiskWriteSensitive, shipmentSummary(result, "create_requested")); err != nil {
			return err
		}
		return enqueueShipmentEvent(ctx, tx, scope, mutation, result, "create_requested")
	})
	return result, fresh, err
}

// ApplyCreateResult commits a normalized adapter result after the worker has
// completed the remote call. The expected version prevents stale workers from
// overwriting a newer state.
func (repository *Repository) ApplyCreateResult(ctx context.Context, scope tenancy.Scope, id logistics.ShipmentID, expectedVersion int64, remote logistics.RemoteResult, mutation logistics.Mutation) (logistics.Shipment, error) {
	if err := validate(ctx, repository, scope); err != nil {
		return logistics.Shipment{}, err
	}
	if !id.Valid() || expectedVersion < 1 || remote.Validate() != nil || remote.Status == logistics.StatusCancelled {
		return logistics.Shipment{}, logistics.ErrInvalidRecord
	}
	if err := mutation.Validate(); err != nil {
		return logistics.Shipment{}, err
	}
	var result logistics.Shipment
	err := repository.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		current, err := loadShipment(ctx, tx, scope, id, true)
		if err != nil {
			return err
		}
		if current.Version != expectedVersion || current.Status != logistics.StatusPending {
			return logistics.ErrConflict
		}
		result, err = updateShipment(ctx, tx, scope, current, remote, mutation.OccurredAt.UTC())
		if err != nil {
			return err
		}
		key, err := pendingReceiptKey(ctx, tx, scope, createOperation, id.String())
		if err != nil {
			return err
		}
		if err := completeReceipt(ctx, tx, scope, createOperation, key, result, remote); err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "fulfillment.shipment.created", id.String(), audit.RiskWriteSensitive, shipmentSummary(result, "created")); err != nil {
			return err
		}
		return enqueueShipmentEvent(ctx, tx, scope, mutation, result, "created")
	})
	return result, err
}

// BeginCancel claims a cancellation command without changing the shipment
// state. State changes happen only after the worker receives a positive,
// normalized carrier result.
func (repository *Repository) BeginCancel(ctx context.Context, scope tenancy.Scope, id logistics.ShipmentID, idempotencyKey string, mutation logistics.Mutation) (logistics.Shipment, bool, error) {
	if err := validate(ctx, repository, scope); err != nil {
		return logistics.Shipment{}, false, err
	}
	if !id.Valid() || !validReference(idempotencyKey) {
		return logistics.Shipment{}, false, logistics.ErrInvalidRecord
	}
	if err := mutation.Validate(); err != nil {
		return logistics.Shipment{}, false, err
	}
	var result logistics.Shipment
	fresh := false
	err := repository.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		current, err := loadShipment(ctx, tx, scope, id, true)
		if err != nil {
			return err
		}
		result = current
		if result.RemoteID == "" {
			return logistics.ErrInvalidState
		}
		digest := cancelDigest(id, result.RemoteID)
		resourceID, claimed, err := claimReceipt(ctx, tx, scope, cancelOperation, idempotencyKey, digest)
		if err != nil {
			return err
		}
		if !claimed {
			result, err = loadShipment(ctx, tx, scope, logistics.ShipmentID(resourceID), false)
			return err
		}
		if result.Status == logistics.StatusDelivered || result.Status == logistics.StatusCancelled || result.Status == logistics.StatusUnknown {
			return logistics.ErrInvalidState
		}
		fresh = true
		if err := bindReceiptResource(ctx, tx, scope, cancelOperation, idempotencyKey, id.String()); err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "fulfillment.shipment.cancel.requested", id.String(), audit.RiskWriteSensitive, shipmentSummary(result, "cancel_requested")); err != nil {
			return err
		}
		return enqueueShipmentEvent(ctx, tx, scope, mutation, result, "cancel_requested")
	})
	return result, fresh, err
}

// ApplyCancelResult commits only a positive normalized cancellation result.
func (repository *Repository) ApplyCancelResult(ctx context.Context, scope tenancy.Scope, id logistics.ShipmentID, expectedVersion int64, remote logistics.RemoteResult, mutation logistics.Mutation) (logistics.Shipment, error) {
	if err := validate(ctx, repository, scope); err != nil {
		return logistics.Shipment{}, err
	}
	if !id.Valid() || expectedVersion < 1 || remote.Validate() != nil || remote.Status != logistics.StatusCancelled {
		return logistics.Shipment{}, logistics.ErrInvalidRecord
	}
	if err := mutation.Validate(); err != nil {
		return logistics.Shipment{}, err
	}
	var result logistics.Shipment
	err := repository.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		current, err := loadShipment(ctx, tx, scope, id, true)
		if err != nil {
			return err
		}
		if current.Version != expectedVersion || current.RemoteID == "" || current.Status == logistics.StatusCancelled || remote.RemoteID != current.RemoteID {
			return logistics.ErrConflict
		}
		row := tx.QueryRowContext(ctx, `UPDATE logistics_shipments SET status=$4,version=version+1,updated_at=$5 WHERE organization_id=$1 AND workspace_id=$2 AND shipment_id=$3 AND version=$6 RETURNING `+shipmentSelect, scope.OrganizationID().String(), scope.WorkspaceID().String(), id.String(), logistics.StatusCancelled, mutation.OccurredAt.UTC(), expectedVersion)
		result, err = scanShipment(row)
		if err != nil {
			return logistics.ErrConflict
		}
		key, err := pendingReceiptKey(ctx, tx, scope, cancelOperation, id.String())
		if err != nil {
			return err
		}
		if err := completeReceipt(ctx, tx, scope, cancelOperation, key, result, remote); err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "fulfillment.shipment.cancelled", id.String(), audit.RiskWriteSensitive, shipmentSummary(result, "cancelled")); err != nil {
			return err
		}
		return enqueueShipmentEvent(ctx, tx, scope, mutation, result, "cancelled")
	})
	return result, err
}

// AppendTrackingEvidence records normalized observation without copying raw
// carrier payloads into the operational database.
func (repository *Repository) AppendTrackingEvidence(ctx context.Context, scope tenancy.Scope, id logistics.ShipmentID, status logistics.Status, observedAt time.Time) error {
	if err := validate(ctx, repository, scope); err != nil {
		return err
	}
	if !id.Valid() || !status.Valid() || observedAt.IsZero() || observedAt.Location() != time.UTC {
		return logistics.ErrInvalidRecord
	}
	return repository.withTx(ctx, scope, false, func(tx *sql.Tx) error {
		if _, err := loadShipment(ctx, tx, scope, id, false); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO logistics_tracking_evidence(organization_id,workspace_id,shipment_id,remote_status,observed_at) VALUES($1,$2,$3,$4,$5)`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id.String(), status, observedAt)
		return err
	})
}

func (repository *Repository) withTx(ctx context.Context, scope tenancy.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly, Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin logistics transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var organizationID, workspaceID string
	if err := tx.QueryRowContext(ctx, applyScopeStatement, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&organizationID, &workspaceID); err != nil {
		return fmt.Errorf("apply logistics tenant scope: %w", err)
	}
	if organizationID != scope.OrganizationID().String() || workspaceID != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit logistics transaction: %w", err)
	}
	return nil
}

func validate(ctx context.Context, repository *Repository, scope tenancy.Scope) error {
	if ctx == nil {
		return logistics.ErrInvalidRecord
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if repository == nil || repository.database == nil {
		return logistics.ErrInvalidRecord
	}
	if !scope.Valid() {
		return logistics.ErrInvalidScope
	}
	return nil
}

func loadShipment(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, id logistics.ShipmentID, forUpdate bool) (logistics.Shipment, error) {
	statement := `SELECT ` + shipmentSelect + ` FROM logistics_shipments WHERE organization_id=$1 AND workspace_id=$2 AND shipment_id=$3`
	if forUpdate {
		statement += ` FOR UPDATE`
	}
	result, err := scanShipment(tx.QueryRowContext(ctx, statement, scope.OrganizationID().String(), scope.WorkspaceID().String(), id.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return logistics.Shipment{}, logistics.ErrNotFound
	}
	if err != nil {
		return logistics.Shipment{}, fmt.Errorf("load shipment: %w", err)
	}
	if result.OrganizationID != scope.OrganizationID().String() || result.WorkspaceID != scope.WorkspaceID().String() {
		return logistics.Shipment{}, logistics.ErrInvalidScope
	}
	return result, nil
}

func scanShipment(row interface{ Scan(...any) error }) (logistics.Shipment, error) {
	var result logistics.Shipment
	var status, currency string
	var minDeliveryAt, maxDeliveryAt sql.NullTime
	if err := row.Scan(&result.ID, &result.OrganizationID, &result.WorkspaceID, &result.AccountID, &result.ExternalID, &result.RemoteID, &result.ServiceCode, &status, &result.TrackingNumber, &result.CostMinorUnits, &currency, &minDeliveryAt, &maxDeliveryAt, &result.Version, &result.UpdatedAt); err != nil {
		return logistics.Shipment{}, err
	}
	result.Status = logistics.Status(status)
	result.Currency = strings.TrimSpace(currency)
	if minDeliveryAt.Valid {
		value := minDeliveryAt.Time.UTC()
		result.MinDeliveryAt = &value
	}
	if maxDeliveryAt.Valid {
		value := maxDeliveryAt.Time.UTC()
		result.MaxDeliveryAt = &value
	}
	result.UpdatedAt = result.UpdatedAt.UTC()
	if err := result.Validate(); err != nil {
		return logistics.Shipment{}, err
	}
	return result, nil
}

func updateShipment(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, current logistics.Shipment, remote logistics.RemoteResult, at time.Time) (logistics.Shipment, error) {
	row := tx.QueryRowContext(ctx, `UPDATE logistics_shipments SET remote_id=$4,status=$5,tracking_number=$6,cost_minor_units=$7,currency=$8,min_delivery_at=$9,max_delivery_at=$10,version=version+1,updated_at=$11 WHERE organization_id=$1 AND workspace_id=$2 AND shipment_id=$3 AND version=$12 RETURNING `+shipmentSelect, scope.OrganizationID().String(), scope.WorkspaceID().String(), current.ID.String(), remote.RemoteID, remote.Status, remote.TrackingNumber, remote.CostMinorUnits, remote.Currency, nullableTime(remote.MinDeliveryAt), nullableTime(remote.MaxDeliveryAt), at, current.Version)
	result, err := scanShipment(row)
	if err != nil {
		return logistics.Shipment{}, logistics.ErrConflict
	}
	return result, nil
}

func claimReceipt(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, operation, key string, digest [32]byte) (string, bool, error) {
	result, err := tx.ExecContext(ctx, `INSERT INTO operation_receipts(organization_id,workspace_id,operation,idempotency_key,request_sha256,state) VALUES($1,$2,$3,$4,$5,'pending') ON CONFLICT DO NOTHING`, scope.OrganizationID().String(), scope.WorkspaceID().String(), operation, key, digest[:])
	if err != nil {
		return "", false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return "", false, err
	}
	if rows == 1 {
		return "", true, nil
	}
	var stored []byte
	var state, resourceID string
	if err := tx.QueryRowContext(ctx, `SELECT request_sha256,state,resource_id FROM operation_receipts WHERE organization_id=$1 AND workspace_id=$2 AND operation=$3 AND idempotency_key=$4 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), operation, key).Scan(&stored, &state, &resourceID); err != nil {
		return "", false, err
	}
	if !bytes.Equal(stored, digest[:]) {
		return "", false, logistics.ErrConflict
	}
	if state == "pending" {
		return "", false, logistics.ErrInProgress
	}
	if state != "completed" || !validReference(resourceID) {
		return "", false, logistics.ErrConflict
	}
	return resourceID, false, nil
}

func bindReceiptResource(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, operation, key, resourceID string) error {
	result, err := tx.ExecContext(ctx, `UPDATE operation_receipts SET resource_type='shipment',resource_id=$5 WHERE organization_id=$1 AND workspace_id=$2 AND operation=$3 AND idempotency_key=$4 AND state='pending'`, scope.OrganizationID().String(), scope.WorkspaceID().String(), operation, key, resourceID)
	if err != nil {
		return fmt.Errorf("bind shipment operation receipt: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return logistics.ErrConflict
	}
	return nil
}

func pendingReceiptKey(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, operation, resourceID string) (string, error) {
	var key string
	err := tx.QueryRowContext(ctx, `SELECT idempotency_key FROM operation_receipts WHERE organization_id=$1 AND workspace_id=$2 AND operation=$3 AND resource_type='shipment' AND resource_id=$4 AND state='pending' ORDER BY created_at LIMIT 1 FOR UPDATE`, scope.OrganizationID().String(), scope.WorkspaceID().String(), operation, resourceID).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", logistics.ErrConflict
	}
	return key, err
}

func completeReceipt(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, operation, key string, shipment logistics.Shipment, remote logistics.RemoteResult) error {
	resultBody, err := json.Marshal(struct {
		ShipmentID string           `json:"shipment_id"`
		Status     logistics.Status `json:"status"`
		Version    int64            `json:"version"`
		RemoteID   string           `json:"remote_id"`
	}{shipment.ID.String(), shipment.Status, shipment.Version, remote.RemoteID})
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE operation_receipts SET state='completed',result=$5::jsonb,completed_at=clock_timestamp() WHERE organization_id=$1 AND workspace_id=$2 AND operation=$3 AND idempotency_key=$4 AND state='pending'`, scope.OrganizationID().String(), scope.WorkspaceID().String(), operation, key, string(resultBody))
	if err != nil {
		return fmt.Errorf("complete shipment operation receipt: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return logistics.ErrConflict
	}
	return nil
}

func appendAudit(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, mutation logistics.Mutation, action, resourceID string, risk audit.Risk, summary audit.Summary) error {
	safe, err := audit.SanitizeSummary(summary)
	if err != nil {
		return err
	}
	record := audit.Record{ID: mutation.AuditID, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), ActorID: mutation.ActorID, Source: mutation.Source, Action: action, ResourceType: "shipment", ResourceID: resourceID, CorrelationID: mutation.CorrelationID, Risk: risk, Summary: safe, CreatedAt: mutation.OccurredAt}
	return auditrepo.AppendTransaction(ctx, tx, scope, record)
}

func enqueueShipmentEvent(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, mutation logistics.Mutation, shipment logistics.Shipment, operation string) error {
	data, err := json.Marshal(struct {
		ShipmentID        string           `json:"shipment_id"`
		Status            logistics.Status `json:"status"`
		Version           int64            `json:"version"`
		Operation         string           `json:"operation"`
		ApprovalRequestID string           `json:"approval_request_id,omitempty"`
	}{shipment.ID.String(), shipment.Status, shipment.Version, operation, mutationApprovalRequestID(mutation, operation)})
	if err != nil {
		return err
	}
	typ, err := eventbus.ParseEventType("commerce.fulfillment.shipment_changed.v1")
	if err != nil {
		return err
	}
	at, err := domain.NewUTCInstant(mutation.OccurredAt)
	if err != nil {
		return err
	}
	event := eventbus.Event{ID: mutation.EventID, Type: typ, OccurredAt: at, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), EntityType: "shipment", EntityID: shipment.ID.String(), Source: mutation.Source, CorrelationID: mutation.CorrelationID, CausationID: mutation.CausationID, ActorID: mutation.ActorID, Data: data}
	if err := event.Validate(); err != nil {
		return err
	}
	enqueuer, err := outboxrepo.NewTransactionEnqueuer(tx)
	if err != nil {
		return err
	}
	return enqueuer.Enqueue(ctx, event)
}

func mutationApprovalRequestID(mutation logistics.Mutation, operation string) string {
	if operation == "cancel_requested" {
		return mutation.ApprovalRequestID
	}
	return ""
}

func shipmentSummary(shipment logistics.Shipment, operation string) audit.Summary {
	return audit.Summary{"shipment_id": shipment.ID.String(), "provider_account_id": shipment.AccountID, "external_id": shipment.ExternalID, "remote_id": shipment.RemoteID, "status": string(shipment.Status), "version": shipment.Version, "operation": operation}
}

func createDigest(command logistics.CreateCommand) [32]byte {
	return sha256.Sum256([]byte(command.ID.String() + "\x00" + command.AccountID + "\x00" + command.ExternalID + "\x00" + command.ServiceCode))
}

func cancelDigest(id logistics.ShipmentID, remoteID string) [32]byte {
	return sha256.Sum256([]byte(id.String() + "\x00" + remoteID))
}

func validReference(value string) bool {
	if value == "" || len(value) > 192 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '.' || character == '_' || character == ':' || character == '/' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}
