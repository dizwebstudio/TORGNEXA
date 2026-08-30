package worker

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/logistics"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

const logisticsShipmentChangedEvent = "commerce.fulfillment.shipment_changed.v1"

type logisticsShipmentStore interface {
	Shipment(context.Context, tenancy.Scope, logistics.ShipmentID) (logistics.Shipment, error)
	ApplyCancelResult(context.Context, tenancy.Scope, logistics.ShipmentID, int64, logistics.RemoteResult, logistics.Mutation) (logistics.Shipment, error)
	ApplyCancelUnknown(context.Context, tenancy.Scope, logistics.ShipmentID, int64, logistics.Mutation) (logistics.Shipment, error)
}

type logisticsCancelAccounts interface {
	AccountByID(context.Context, string, string, string) (sdk.Account, error)
	AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error)
}

type logisticsCancelApprovals interface {
	Request(context.Context, tenancy.Scope, string) (approval.Request, error)
	BeginExecution(context.Context, tenancy.Scope, approval.TransitionCommand) (approval.Request, error)
	CompleteExecution(context.Context, tenancy.Scope, approval.TransitionCommand, bool) (approval.Request, error)
}

type logisticsCancelRuntime interface {
	logisticsCanceler(context.Context, tenancy.Scope, sdk.Account, sdk.Runtime) (sdk.LogisticsShipmentCanceler, error)
}

type logisticsRuntimeFactory func(context.Context, tenancy.Scope, sdk.Account) (sdk.Runtime, error)

type logisticsCancelRoute struct {
	shipments  logisticsShipmentStore
	accounts   logisticsCancelAccounts
	approvals  logisticsCancelApprovals
	runtime    logisticsCancelRuntime
	newRuntime logisticsRuntimeFactory
}

func newLogisticsCancelRoute(shipments logisticsShipmentStore, accounts logisticsCancelAccounts, approvals logisticsCancelApprovals, runtime logisticsCancelRuntime, newRuntime logisticsRuntimeFactory) (*logisticsCancelRoute, error) {
	if shipments == nil || accounts == nil || approvals == nil || runtime == nil || newRuntime == nil {
		return nil, errors.New("worker: logistics cancellation dependencies required")
	}
	return &logisticsCancelRoute{shipments: shipments, accounts: accounts, approvals: approvals, runtime: runtime, newRuntime: newRuntime}, nil
}

func (route *logisticsCancelRoute) Handle(ctx context.Context, delivery eventbus.Delivery) error {
	if route == nil || ctx == nil {
		return eventbus.Permanent("logistics_cancel_invalid_route")
	}
	if delivery.Event.Type.String() != logisticsShipmentChangedEvent {
		return nil
	}
	var payload logisticsShipmentChangedPayload
	if err := decodeCommerceEvent(delivery.Event.Data, &payload); err != nil || payload.ShipmentID != delivery.Event.EntityID || payload.Operation != "cancel_requested" || payload.ApprovalRequestID == "" || payload.Version < 1 || !logistics.Status(payload.Status).Valid() {
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
		execution, err := route.prepareApproval(ctx, scope, shipmentID, payload.ApprovalRequestID, delivery)
		if err != nil {
			return err
		}
		if err := route.completeApproval(ctx, scope, execution, delivery, true, ""); err != nil {
			return eventbus.Retryable("logistics_cancel_approval_complete_failed")
		}
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
	if account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
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
	execution, err := route.prepareApproval(ctx, scope, shipmentID, payload.ApprovalRequestID, delivery)
	if err != nil {
		return err
	}
	remote, err := canceler.CancelLogisticsShipment(ctx, account, runtime, sdk.ShipmentCancelRequest{RemoteID: shipment.RemoteID, IdempotencyKey: stableID("shipment_cancel_", delivery.Event.ID)})
	if err != nil {
		return route.finishRemoteError(ctx, scope, execution, delivery, shipmentID, payload.Version, err)
	}
	normalized, err := normalizeCancelledShipment(remote, shipment.RemoteID)
	if err != nil {
		if completeErr := route.completeApproval(ctx, scope, execution, delivery, false, "logistics_cancel_invalid_remote_result"); completeErr != nil {
			return eventbus.Retryable("logistics_cancel_approval_complete_failed")
		}
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
	if err := route.completeApproval(ctx, scope, execution, delivery, true, ""); err != nil {
		return eventbus.Retryable("logistics_cancel_approval_complete_failed")
	}
	return nil
}

type logisticsShipmentChangedPayload struct {
	ShipmentID        string `json:"shipment_id"`
	Status            string `json:"status"`
	Version           int64  `json:"version"`
	Operation         string `json:"operation"`
	ApprovalRequestID string `json:"approval_request_id"`
}

func (route *logisticsCancelRoute) prepareApproval(ctx context.Context, scope tenancy.Scope, shipmentID logistics.ShipmentID, approvalID string, delivery eventbus.Delivery) (approval.Request, error) {
	request, err := route.approvals.Request(ctx, scope, approvalID)
	if err != nil {
		if errors.Is(err, approval.ErrInvalid) {
			return approval.Request{}, eventbus.Permanent("logistics_cancel_approval_missing")
		}
		return approval.Request{}, eventbus.Retryable("logistics_cancel_approval_read_failed")
	}
	if request.Action != "fulfillment.shipment.cancel" || request.ResourceType != "shipment" || request.ResourceID != shipmentID.String() || request.Risk != approval.RiskWriteSensitive {
		return approval.Request{}, eventbus.Permanent("logistics_cancel_approval_mismatch")
	}
	if request.State == approval.StateApproved {
		started, err := route.approvals.BeginExecution(ctx, scope, approval.TransitionCommand{RequestID: request.ID, ExpectedVersion: request.Version, Mutation: logisticsApprovalMutation(delivery, "begin")})
		if err != nil {
			return approval.Request{}, eventbus.Retryable("logistics_cancel_approval_start_failed")
		}
		request = started
	}
	if request.State != approval.StateExecuting {
		return approval.Request{}, eventbus.Permanent("logistics_cancel_approval_not_executable")
	}
	if _, err := approval.Grant(request, "fulfillment.shipment.cancel", "shipment", shipmentID.String()); err != nil {
		return approval.Request{}, eventbus.Permanent("logistics_cancel_approval_denied")
	}
	return request, nil
}

func (route *logisticsCancelRoute) finishRemoteError(ctx context.Context, scope tenancy.Scope, request approval.Request, delivery eventbus.Delivery, shipmentID logistics.ShipmentID, expectedVersion int64, remoteErr error) error {
	if isLogisticsCancelUnknown(remoteErr) {
		if _, err := route.shipments.ApplyCancelUnknown(ctx, scope, shipmentID, expectedVersion, logistics.Mutation{EventID: stableUUID("shipment_cancel_unknown_event:" + delivery.Event.ID), AuditID: stableUUID("shipment_cancel_unknown_audit:" + delivery.Event.ID), ActorID: "system:logistics", Source: "worker.logistics", CorrelationID: delivery.Event.CorrelationID, CausationID: delivery.Event.ID, OccurredAt: time.Now().UTC()}); err != nil {
			return eventbus.Retryable("logistics_cancel_unknown_persist_failed")
		}
		if err := route.completeApproval(ctx, scope, request, delivery, false, "logistics_cancel_outcome_unknown"); err != nil {
			return eventbus.Retryable("logistics_cancel_approval_complete_failed")
		}
		return eventbus.Permanent("logistics_cancel_outcome_unknown")
	}
	classified := classifyLogisticsCancelError(remoteErr)
	class, _ := eventbus.ClassifyFailure(classified)
	if class == eventbus.FailureRetryable {
		return classified
	}
	if err := route.completeApproval(ctx, scope, request, delivery, false, "logistics_cancel_remote_rejected"); err != nil {
		return eventbus.Retryable("logistics_cancel_approval_complete_failed")
	}
	return classified
}

func isLogisticsCancelUnknown(err error) bool {
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote == nil {
		return true
	}
	return remote.Category == sdk.ErrorTimeout
}

func (route *logisticsCancelRoute) completeApproval(ctx context.Context, scope tenancy.Scope, request approval.Request, delivery eventbus.Delivery, success bool, failureCode string) error {
	if request.State != approval.StateExecuting {
		return nil
	}
	_, err := route.approvals.CompleteExecution(ctx, scope, approval.TransitionCommand{RequestID: request.ID, ExpectedVersion: request.Version, FailureCode: failureCode, Mutation: logisticsApprovalMutation(delivery, "complete")}, success)
	return err
}

func logisticsApprovalMutation(delivery eventbus.Delivery, phase string) approval.Mutation {
	key := "logistics.cancel.approval:" + delivery.Event.ID + ":" + phase
	return approval.Mutation{AuditID: stableUUID(key + ":audit"), EventID: stableUUID(key + ":event"), ActorID: "system:logistics", Source: "worker.logistics", CorrelationID: delivery.Event.CorrelationID, CausationID: delivery.Event.ID, OccurredAt: time.Now().UTC()}
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
