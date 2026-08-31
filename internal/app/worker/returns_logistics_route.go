package worker

import (
	"context"
	"errors"
	"strings"
	"time"

	core "github.com/torgnexa/torgnexa/internal/core/returns"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

const returnLogisticsRequestedEvent = "commerce.returns.logistics_requested.v1"

type returnLogisticsStore interface {
	core.Repository
}

type returnLogisticsRuntime interface {
	logisticsReturnCreator(context.Context, tenancy.Scope, sdk.Account, sdk.Runtime) (sdk.LogisticsReturnCreator, error)
}

type returnLogisticsRoute struct {
	operations returnLogisticsStore
	accounts   logisticsCancelAccounts
	runtime    returnLogisticsRuntime
	newRuntime logisticsRuntimeFactory
}

func newReturnLogisticsRoute(operations returnLogisticsStore, accounts logisticsCancelAccounts, runtime returnLogisticsRuntime, newRuntime logisticsRuntimeFactory) (*returnLogisticsRoute, error) {
	if operations == nil || accounts == nil || runtime == nil || newRuntime == nil {
		return nil, errors.New("worker: return logistics dependencies required")
	}
	return &returnLogisticsRoute{operations: operations, accounts: accounts, runtime: runtime, newRuntime: newRuntime}, nil
}

func (route *returnLogisticsRoute) Handle(ctx context.Context, delivery eventbus.Delivery) error {
	if route == nil || ctx == nil {
		return eventbus.Permanent("return_logistics_invalid_route")
	}
	if delivery.Event.Type.String() != returnLogisticsRequestedEvent {
		return nil
	}
	var payload struct {
		OperationID string `json:"operation_id"`
		ReturnID    string `json:"return_id"`
		Version     int64  `json:"version"`
	}
	if err := decodeCommerceEvent(delivery.Event.Data, &payload); err != nil || payload.OperationID != delivery.Event.EntityID || payload.ReturnID == "" || payload.Version < 1 {
		return eventbus.Permanent("return_logistics_invalid_payload")
	}
	scope, err := core.ParseScope(delivery.Event.OrganizationID, delivery.Event.WorkspaceID)
	if err != nil {
		return eventbus.Permanent("return_logistics_invalid_scope")
	}
	operationID, err := core.ParseReturnLogisticsOperationID(payload.OperationID)
	if err != nil {
		return eventbus.Permanent("return_logistics_invalid_operation")
	}
	operation, err := route.operations.ReturnLogistics(ctx, scope, operationID)
	if errors.Is(err, core.ErrNotFound) {
		return eventbus.Permanent("return_logistics_operation_missing")
	}
	if err != nil {
		return eventbus.Retryable("return_logistics_operation_read_failed")
	}
	if operation.ReturnID.String() != payload.ReturnID {
		return eventbus.Permanent("return_logistics_return_mismatch")
	}
	operation, fresh, err := route.operations.BeginReturnLogistics(ctx, scope, operationID, core.Mutation{EventID: stableID("return_logistics_begin_", delivery.Event.ID), AuditID: stableUUID("return_logistics_begin_audit:" + delivery.Event.ID), ActorID: "system:logistics", Source: "worker.logistics", CorrelationID: delivery.Event.CorrelationID, CausationID: delivery.Event.ID, OccurredAt: time.Now().UTC()})
	if err != nil {
		return eventbus.Retryable("return_logistics_begin_failed")
	}
	if !fresh {
		return nil
	}
	if operation.Status != core.ReturnLogisticsExecuting || operation.Version < 2 {
		return eventbus.Permanent("return_logistics_invalid_execution_state")
	}
	tenantScope, err := tenancy.ParseScope(scope.OrganizationID(), scope.WorkspaceID())
	if err != nil {
		return eventbus.Permanent("return_logistics_invalid_scope")
	}
	account, err := route.accounts.AccountByID(ctx, tenantScope.OrganizationID().String(), tenantScope.WorkspaceID().String(), operation.ConnectorAccountID)
	if errors.Is(err, sdk.ErrAccountNotFound) {
		return route.fail(ctx, scope, operation, "connector_account_missing", delivery)
	}
	if err != nil {
		return eventbus.Retryable("return_logistics_account_read_failed")
	}
	if account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		return route.fail(ctx, scope, operation, "connector_unavailable", delivery)
	}
	settings, err := route.accounts.AccountCapabilities(ctx, tenantScope, account.ID)
	if err != nil {
		return eventbus.Retryable("return_logistics_capability_read_failed")
	}
	if !sdk.CapabilityEnabled(settings, sdk.Capability("logistics.return.create")) {
		return route.fail(ctx, scope, operation, "capability_disabled", delivery)
	}
	runtime, err := route.newRuntime(ctx, tenantScope, account)
	if err != nil {
		return eventbus.Retryable("return_logistics_runtime_failed")
	}
	creator, err := route.runtime.logisticsReturnCreator(ctx, tenantScope, account, runtime)
	if err != nil {
		return route.fail(ctx, scope, operation, "operation_unavailable", delivery)
	}
	remote, err := creator.CreateLogisticsReturn(ctx, account, runtime, sdk.ReturnCreateRequest{OriginalRemoteID: operation.OriginalRemoteID, ExternalID: operation.ExternalID, MailType: operation.MailType, TariffCode: operation.TariffCode, IdempotencyKey: stableID("return_logistics_remote_", delivery.Event.ID)})
	if err != nil {
		if isReturnLogisticsUnknown(err) {
			return route.unknown(ctx, scope, operation, delivery)
		}
		if classified := classifyReturnLogisticsError(err); classified != nil {
			if class, _ := eventbus.ClassifyFailure(classified); class == eventbus.FailureRetryable {
				return classified
			}
		}
		return route.fail(ctx, scope, operation, "remote_rejected", delivery)
	}
	result := core.ReturnLogisticsResult{RemoteID: strings.TrimSpace(remote.RemoteID), Status: remote.Status, TrackingNumber: strings.TrimSpace(remote.TrackingNumber), ObservedAt: remote.ObservedAt.UTC()}
	if result.Validate() != nil {
		return route.fail(ctx, scope, operation, "invalid_remote_result", delivery)
	}
	if _, err := route.operations.ApplyReturnLogisticsResult(ctx, scope, operation.ID, operation.Version, result, returnLogisticsMutation(delivery, "succeeded")); err != nil {
		return eventbus.Retryable("return_logistics_result_persist_failed")
	}
	return nil
}

func (route *returnLogisticsRoute) fail(ctx context.Context, scope core.Scope, operation core.ReturnLogisticsOperation, code string, delivery eventbus.Delivery) error {
	if _, err := route.operations.ApplyReturnLogisticsFailure(ctx, scope, operation.ID, operation.Version, code, returnLogisticsMutation(delivery, "failed")); err != nil {
		return eventbus.Retryable("return_logistics_failure_persist_failed")
	}
	return eventbus.Permanent("return_logistics_" + code)
}

func (route *returnLogisticsRoute) unknown(ctx context.Context, scope core.Scope, operation core.ReturnLogisticsOperation, delivery eventbus.Delivery) error {
	if _, err := route.operations.ApplyReturnLogisticsUnknown(ctx, scope, operation.ID, operation.Version, returnLogisticsMutation(delivery, "unknown")); err != nil {
		return eventbus.Retryable("return_logistics_unknown_persist_failed")
	}
	return eventbus.Permanent("return_logistics_outcome_unknown")
}

func returnLogisticsMutation(delivery eventbus.Delivery, phase string) core.Mutation {
	key := "return.logistics." + phase + ":" + delivery.Event.ID
	return core.Mutation{EventID: stableUUID(key + ":event"), AuditID: stableUUID(key + ":audit"), ActorID: "system:logistics", Source: "worker.logistics", CorrelationID: delivery.Event.CorrelationID, CausationID: delivery.Event.ID, OccurredAt: time.Now().UTC()}
}

func isReturnLogisticsUnknown(err error) bool {
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote == nil {
		return true
	}
	return remote.Category == sdk.ErrorTimeout
}

func classifyReturnLogisticsError(err error) error {
	var remote *sdk.RemoteError
	if errors.As(err, &remote) && remote != nil && remote.Retryable() {
		return eventbus.Retryable("return_logistics_remote_retryable")
	}
	if errors.As(err, &remote) {
		return eventbus.Permanent("return_logistics_remote_rejected")
	}
	return eventbus.Retryable("return_logistics_remote_unavailable")
}
