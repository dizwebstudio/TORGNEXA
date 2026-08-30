package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/logistics"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

type logisticsCreateShipmentStore interface {
	Shipment(context.Context, tenancy.Scope, logistics.ShipmentID) (logistics.Shipment, error)
	CreateRequestReference(context.Context, tenancy.Scope, logistics.ShipmentID) (string, error)
	ApplyCreateResult(context.Context, tenancy.Scope, logistics.ShipmentID, int64, logistics.RemoteResult, logistics.Mutation) (logistics.Shipment, error)
	ApplyCreateUnknown(context.Context, tenancy.Scope, logistics.ShipmentID, int64, logistics.Mutation) (logistics.Shipment, error)
}

type logisticsCreateAccounts interface {
	AccountByID(context.Context, string, string, string) (sdk.Account, error)
	AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error)
}

type logisticsCreateApprovals interface {
	Request(context.Context, tenancy.Scope, string) (approval.Request, error)
	BeginExecution(context.Context, tenancy.Scope, approval.TransitionCommand) (approval.Request, error)
	CompleteExecution(context.Context, tenancy.Scope, approval.TransitionCommand, bool) (approval.Request, error)
}

type logisticsCreateRuntime interface {
	logisticsCreator(context.Context, tenancy.Scope, sdk.Account, sdk.Runtime) (sdk.LogisticsShipmentCreator, error)
}

type logisticsCreateRoute struct {
	shipments       logisticsCreateShipmentStore
	accounts        logisticsCreateAccounts
	approvals       logisticsCreateApprovals
	runtime         logisticsCreateRuntime
	credentialStore secrets.SecretProvider
	newRuntime      logisticsRuntimeFactory
}

func newLogisticsCreateRoute(shipments logisticsCreateShipmentStore, accounts logisticsCreateAccounts, approvals logisticsCreateApprovals, runtime logisticsCreateRuntime, credentialStore secrets.SecretProvider, newRuntime logisticsRuntimeFactory) (*logisticsCreateRoute, error) {
	if shipments == nil || accounts == nil || approvals == nil || runtime == nil || credentialStore == nil || newRuntime == nil {
		return nil, errors.New("worker: logistics creation dependencies required")
	}
	return &logisticsCreateRoute{shipments: shipments, accounts: accounts, approvals: approvals, runtime: runtime, credentialStore: credentialStore, newRuntime: newRuntime}, nil
}

func (route *logisticsCreateRoute) Handle(ctx context.Context, delivery eventbus.Delivery) error {
	if route == nil || ctx == nil {
		return eventbus.Permanent("logistics_create_invalid_route")
	}
	if delivery.Event.Type.String() != logisticsShipmentChangedEvent {
		return nil
	}
	var payload logisticsShipmentChangedPayload
	if err := decodeCommerceEvent(delivery.Event.Data, &payload); err != nil || payload.ShipmentID != delivery.Event.EntityID || payload.Operation != "create_requested" || payload.ApprovalRequestID == "" || payload.Version < 1 || payload.Status != string(logistics.StatusPending) {
		return eventbus.Permanent("logistics_create_invalid_payload")
	}
	scope, err := tenancy.ParseScope(delivery.Event.OrganizationID, delivery.Event.WorkspaceID)
	if err != nil {
		return eventbus.Permanent("logistics_create_invalid_scope")
	}
	shipmentID, err := logistics.ParseShipmentID(payload.ShipmentID)
	if err != nil {
		return eventbus.Permanent("logistics_create_invalid_shipment")
	}
	shipment, err := route.shipments.Shipment(ctx, scope, shipmentID)
	if errors.Is(err, logistics.ErrNotFound) {
		return eventbus.Permanent("logistics_create_shipment_missing")
	}
	if err != nil {
		return eventbus.Retryable("logistics_create_shipment_read_failed")
	}
	if shipment.Version != payload.Version || shipment.Status != logistics.StatusPending {
		return eventbus.Permanent("logistics_create_stale_or_invalid_state")
	}
	account, err := route.accounts.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), shipment.AccountID)
	if errors.Is(err, sdk.ErrAccountNotFound) {
		return eventbus.Permanent("logistics_create_account_missing")
	}
	if err != nil {
		return eventbus.Retryable("logistics_create_account_read_failed")
	}
	if account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		return eventbus.Permanent("logistics_create_connector_unavailable")
	}
	settings, err := route.accounts.AccountCapabilities(ctx, scope, account.ID)
	if err != nil {
		return eventbus.Retryable("logistics_create_capability_read_failed")
	}
	if !sdk.CapabilityEnabled(settings, sdk.Capability("logistics.shipment.create")) {
		return eventbus.Permanent("logistics_create_capability_disabled")
	}
	payloadReference, err := route.shipments.CreateRequestReference(ctx, scope, shipmentID)
	if errors.Is(err, logistics.ErrNotFound) || errors.Is(err, logistics.ErrConflict) {
		return eventbus.Permanent("logistics_create_payload_reference_missing")
	}
	if err != nil {
		return eventbus.Retryable("logistics_create_payload_reference_read_failed")
	}
	reference, err := secrets.ParseReference(payloadReference)
	if err != nil {
		return eventbus.Permanent("logistics_create_payload_reference_invalid")
	}
	request, err := route.readPayload(ctx, scope, reference, shipment)
	if err != nil {
		return eventbus.Retryable("logistics_create_payload_read_failed")
	}
	execution, err := route.prepareApproval(ctx, scope, shipmentID, payload.ApprovalRequestID, delivery)
	if err != nil {
		return err
	}
	runtime, err := route.newRuntime(ctx, scope, account)
	if err != nil {
		return eventbus.Retryable("logistics_create_runtime_failed")
	}
	creator, err := route.runtime.logisticsCreator(ctx, scope, account, runtime)
	if err != nil {
		return eventbus.Permanent("logistics_create_operation_unavailable")
	}
	remote, err := creator.CreateLogisticsShipment(ctx, account, runtime, request)
	if err != nil {
		return route.finishUnknown(ctx, scope, shipmentID, payload.Version, execution, delivery)
	}
	normalized, err := normalizeCreatedShipment(remote)
	if err != nil {
		return route.finishUnknown(ctx, scope, shipmentID, payload.Version, execution, delivery)
	}
	if _, err := route.shipments.ApplyCreateResult(ctx, scope, shipmentID, payload.Version, normalized, logistics.Mutation{EventID: stableUUID("shipment_create_event:" + delivery.Event.ID), AuditID: stableUUID("shipment_create_audit:" + delivery.Event.ID), ActorID: "system:logistics", Source: "worker.logistics", CorrelationID: delivery.Event.CorrelationID, CausationID: delivery.Event.ID, OccurredAt: time.Now().UTC()}); err != nil {
		return eventbus.Retryable("logistics_create_persist_failed")
	}
	if err := route.completeApproval(ctx, scope, execution, delivery, true, ""); err != nil {
		return eventbus.Retryable("logistics_create_approval_complete_failed")
	}
	return nil
}

func (route *logisticsCreateRoute) readPayload(ctx context.Context, scope tenancy.Scope, reference secrets.Reference, shipment logistics.Shipment) (sdk.ShipmentCreateRequest, error) {
	var request sdk.ShipmentCreateRequest
	err := route.credentialStore.Use(ctx, scope, reference, func(material []byte) error {
		decoder := json.NewDecoder(strings.NewReader(string(material)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return err
		}
		var trailing struct{}
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return err
		}
		if request.Validate() != nil || len(request.Parcels) > 50 || !validLogisticsContactForWorker(request.Sender) || !validLogisticsContactForWorker(request.Recipient) || request.ExternalID != shipment.ExternalID || request.ServiceCode != shipment.ServiceCode || request.IdempotencyKey == "" {
			return logistics.ErrInvalidRecord
		}
		return nil
	})
	return request, err
}

func validLogisticsContactForWorker(contact sdk.LogisticsContact) bool {
	return strings.TrimSpace(contact.Name) != "" && strings.TrimSpace(contact.Name) == contact.Name && len(contact.Name) <= 255 && strings.TrimSpace(contact.Phone) != "" && strings.TrimSpace(contact.Phone) == contact.Phone && len(contact.Phone) <= 32 && strings.TrimSpace(contact.Email) == contact.Email && len(contact.Email) <= 254 && !strings.ContainsAny(contact.Email, "\r\n\t ")
}

func (route *logisticsCreateRoute) prepareApproval(ctx context.Context, scope tenancy.Scope, shipmentID logistics.ShipmentID, approvalID string, delivery eventbus.Delivery) (approval.Request, error) {
	request, err := route.approvals.Request(ctx, scope, approvalID)
	if err != nil {
		if errors.Is(err, approval.ErrInvalid) {
			return approval.Request{}, eventbus.Permanent("logistics_create_approval_missing")
		}
		return approval.Request{}, eventbus.Retryable("logistics_create_approval_read_failed")
	}
	if request.Action != "fulfillment.shipment.create" || request.ResourceType != "shipment" || request.ResourceID != shipmentID.String() || request.Risk != approval.RiskWriteSensitive {
		return approval.Request{}, eventbus.Permanent("logistics_create_approval_mismatch")
	}
	if request.State == approval.StateApproved {
		started, err := route.approvals.BeginExecution(ctx, scope, approval.TransitionCommand{RequestID: request.ID, ExpectedVersion: request.Version, Mutation: logisticsApprovalMutation(delivery, "create_begin")})
		if err != nil {
			return approval.Request{}, eventbus.Retryable("logistics_create_approval_start_failed")
		}
		request = started
	}
	if request.State != approval.StateExecuting {
		return approval.Request{}, eventbus.Permanent("logistics_create_approval_not_executable")
	}
	if _, err := approval.Grant(request, "fulfillment.shipment.create", "shipment", shipmentID.String()); err != nil {
		return approval.Request{}, eventbus.Permanent("logistics_create_approval_denied")
	}
	return request, nil
}

func (route *logisticsCreateRoute) finishUnknown(ctx context.Context, scope tenancy.Scope, shipmentID logistics.ShipmentID, expectedVersion int64, request approval.Request, delivery eventbus.Delivery) error {
	if _, err := route.shipments.ApplyCreateUnknown(ctx, scope, shipmentID, expectedVersion, logistics.Mutation{EventID: stableUUID("shipment_create_unknown_event:" + delivery.Event.ID), AuditID: stableUUID("shipment_create_unknown_audit:" + delivery.Event.ID), ActorID: "system:logistics", Source: "worker.logistics", CorrelationID: delivery.Event.CorrelationID, CausationID: delivery.Event.ID, OccurredAt: time.Now().UTC()}); err != nil {
		return eventbus.Retryable("logistics_create_unknown_persist_failed")
	}
	if err := route.completeApproval(ctx, scope, request, delivery, false, "logistics_create_outcome_unknown"); err != nil {
		return eventbus.Retryable("logistics_create_approval_complete_failed")
	}
	return eventbus.Permanent("logistics_create_outcome_unknown")
}

func (route *logisticsCreateRoute) completeApproval(ctx context.Context, scope tenancy.Scope, request approval.Request, delivery eventbus.Delivery, success bool, failureCode string) error {
	if request.State != approval.StateExecuting {
		return nil
	}
	_, err := route.approvals.CompleteExecution(ctx, scope, approval.TransitionCommand{RequestID: request.ID, ExpectedVersion: request.Version, FailureCode: failureCode, Mutation: logisticsApprovalMutation(delivery, "create_complete")}, success)
	return err
}

func normalizeCreatedShipment(result sdk.ShipmentResult) (logistics.RemoteResult, error) {
	if strings.ToLower(strings.TrimSpace(result.Status)) != string(logistics.StatusCreated) || result.Cost.Validate() != nil || result.ObservedAt.IsZero() {
		return logistics.RemoteResult{}, logistics.ErrInvalidRecord
	}
	observedAt := result.ObservedAt.UTC()
	return logistics.RemoteResult{RemoteID: strings.TrimSpace(result.RemoteID), Status: logistics.StatusCreated, TrackingNumber: strings.TrimSpace(result.TrackingNumber), CostMinorUnits: result.Cost.MinorUnits, Currency: strings.ToUpper(strings.TrimSpace(result.Cost.Currency)), ObservedAt: observedAt}, nil
}
