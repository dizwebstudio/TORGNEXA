package worker

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/logistics"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

const logisticsShipmentChangedEvent = "commerce.fulfillment.shipment_changed.v1"

type logisticsShipmentStore interface {
	Shipment(context.Context, tenancy.Scope, logistics.ShipmentID) (logistics.Shipment, error)
	ApplyCancelResult(context.Context, tenancy.Scope, logistics.ShipmentID, int64, logistics.RemoteResult, logistics.Mutation) (logistics.Shipment, error)
}

type logisticsCancelAccounts interface {
	AccountByID(context.Context, string, string, string) (sdk.Account, error)
	AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error)
}

type logisticsCancelRuntime interface {
	logisticsCanceler(context.Context, tenancy.Scope, sdk.Account, sdk.Runtime) (sdk.LogisticsShipmentCanceler, error)
}

type logisticsRuntimeFactory func(context.Context, tenancy.Scope, sdk.Account) (sdk.Runtime, error)

type logisticsCancelRoute struct {
	shipments  logisticsShipmentStore
	accounts   logisticsCancelAccounts
	runtime    logisticsCancelRuntime
	newRuntime logisticsRuntimeFactory
}

func newLogisticsCancelRoute(shipments logisticsShipmentStore, accounts logisticsCancelAccounts, runtime logisticsCancelRuntime, newRuntime logisticsRuntimeFactory) (*logisticsCancelRoute, error) {
	if shipments == nil || accounts == nil || runtime == nil || newRuntime == nil {
		return nil, errors.New("worker: logistics cancellation dependencies required")
	}
	return &logisticsCancelRoute{shipments: shipments, accounts: accounts, runtime: runtime, newRuntime: newRuntime}, nil
}

func (route *logisticsCancelRoute) Handle(ctx context.Context, delivery eventbus.Delivery) error {
	if route == nil || ctx == nil {
		return eventbus.Permanent("logistics_cancel_invalid_route")
	}
	if delivery.Event.Type.String() != logisticsShipmentChangedEvent {
		return nil
	}
	var payload logisticsShipmentChangedPayload
	if err := decodeCommerceEvent(delivery.Event.Data, &payload); err != nil || payload.ShipmentID != delivery.Event.EntityID || payload.Operation != "cancel_requested" || payload.Version < 1 || !logistics.Status(payload.Status).Valid() {
		return eventbus.Permanent("logistics_cancel_invalid_payload")
	}
	scope, err := tenancy.ParseScope(delivery.Event.OrganizationID, delivery.Event.WorkspaceID)
	if err != nil {
		return eventbus.Permanent("logistics_cancel_invalid_scope")
	}
	shipmentID, err := logistics.ParseShipmentID(payload.ShipmentID)
	if err != nil {
		return eventbus.Permanent("logistics_cancel_invalid_shipment")
	}
	shipment, err := route.shipments.Shipment(ctx, scope, shipmentID)
	if errors.Is(err, logistics.ErrNotFound) {
		return eventbus.Permanent("logistics_cancel_shipment_missing")
	}
	if err != nil {
		return eventbus.Retryable("logistics_cancel_shipment_read_failed")
	}
	if shipment.Status == logistics.StatusCancelled {
		return nil
	}
	if shipment.Version != payload.Version || shipment.Status == logistics.StatusDelivered || shipment.Status == logistics.StatusUnknown || shipment.RemoteID == "" {
		return eventbus.Permanent("logistics_cancel_stale_or_invalid_state")
	}
	account, err := route.accounts.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), shipment.AccountID)
	if errors.Is(err, sdk.ErrAccountNotFound) {
		return eventbus.Permanent("logistics_cancel_account_missing")
	}
	if err != nil {
		return eventbus.Retryable("logistics_cancel_account_read_failed")
	}
	if account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive || account.ConnectorID != "cdek" {
		return eventbus.Permanent("logistics_cancel_connector_unavailable")
	}
	settings, err := route.accounts.AccountCapabilities(ctx, scope, account.ID)
	if err != nil {
		return eventbus.Retryable("logistics_cancel_capability_read_failed")
	}
	if !sdk.CapabilityEnabled(settings, sdk.Capability("logistics.shipment.cancel")) {
		return eventbus.Permanent("logistics_cancel_capability_disabled")
	}
	runtime, err := route.newRuntime(ctx, scope, account)
	if err != nil {
		return eventbus.Retryable("logistics_cancel_runtime_failed")
	}
	canceler, err := route.runtime.logisticsCanceler(ctx, scope, account, runtime)
	if err != nil {
		return eventbus.Permanent("logistics_cancel_operation_unavailable")
	}
	remote, err := canceler.CancelLogisticsShipment(ctx, account, runtime, sdk.ShipmentCancelRequest{RemoteID: shipment.RemoteID, IdempotencyKey: stableID("shipment_cancel_", delivery.Event.ID)})
	if err != nil {
		return classifyLogisticsCancelError(err)
	}
	normalized, err := normalizeCancelledShipment(remote, shipment.RemoteID)
	if err != nil {
		return eventbus.Permanent("logistics_cancel_invalid_remote_result")
	}
	_, err = route.shipments.ApplyCancelResult(ctx, scope, shipmentID, payload.Version, normalized, logistics.Mutation{
		EventID:       stableUUID("shipment_cancel_event:" + delivery.Event.ID),
		AuditID:       stableUUID("shipment_cancel_audit:" + delivery.Event.ID),
		ActorID:       "system:logistics",
		Source:        "worker.logistics",
		CorrelationID: delivery.Event.CorrelationID,
		CausationID:   delivery.Event.ID,
		OccurredAt:    time.Now().UTC(),
	})
	if err != nil {
		return eventbus.Retryable("logistics_cancel_persist_failed")
	}
	return nil
}

type logisticsShipmentChangedPayload struct {
	ShipmentID string `json:"shipment_id"`
	Status     string `json:"status"`
	Version    int64  `json:"version"`
	Operation  string `json:"operation"`
}

func normalizeCancelledShipment(result sdk.ShipmentResult, expectedRemoteID string) (logistics.RemoteResult, error) {
	if result.RemoteID != expectedRemoteID || strings.ToLower(strings.TrimSpace(result.Status)) != string(logistics.StatusCancelled) || result.Cost.Validate() != nil || result.ObservedAt.IsZero() {
		return logistics.RemoteResult{}, logistics.ErrInvalidRecord
	}
	observedAt := result.ObservedAt.UTC()
	return logistics.RemoteResult{RemoteID: result.RemoteID, Status: logistics.StatusCancelled, TrackingNumber: result.TrackingNumber, CostMinorUnits: result.Cost.MinorUnits, Currency: strings.ToUpper(result.Cost.Currency), ObservedAt: observedAt}, nil
}

func classifyLogisticsCancelError(err error) error {
	var remote *sdk.RemoteError
	if errors.As(err, &remote) && remote != nil && remote.Retryable() {
		return eventbus.Retryable("logistics_cancel_remote_retryable")
	}
	if errors.As(err, &remote) {
		return eventbus.Permanent("logistics_cancel_remote_rejected")
	}
	return eventbus.Retryable("logistics_cancel_remote_unavailable")
}
