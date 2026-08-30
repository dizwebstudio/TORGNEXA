package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/logistics"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

type logisticsCancelShipmentStub struct {
	shipment logistics.Shipment
	applied  int
	unknown  int
}

func (stub *logisticsCancelShipmentStub) Shipment(context.Context, tenancy.Scope, logistics.ShipmentID) (logistics.Shipment, error) {
	return stub.shipment, nil
}

func (stub *logisticsCancelShipmentStub) ApplyCancelResult(_ context.Context, _ tenancy.Scope, _ logistics.ShipmentID, _ int64, result logistics.RemoteResult, _ logistics.Mutation) (logistics.Shipment, error) {
	stub.applied++
	stub.shipment.Status = result.Status
	stub.shipment.Version++
	return stub.shipment, nil
}

func (stub *logisticsCancelShipmentStub) ApplyCancelUnknown(_ context.Context, _ tenancy.Scope, _ logistics.ShipmentID, _ int64, _ logistics.Mutation) (logistics.Shipment, error) {
	stub.unknown++
	stub.shipment.Status = logistics.StatusUnknown
	stub.shipment.Version++
	return stub.shipment, nil
}

type logisticsCancelAccountStub struct {
	account  sdk.Account
	settings []sdk.AccountCapabilitySetting
}

func (stub logisticsCancelAccountStub) AccountByID(context.Context, string, string, string) (sdk.Account, error) {
	return stub.account, nil
}

func (stub logisticsCancelAccountStub) AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error) {
	return stub.settings, nil
}

type logisticsCancelRuntimeStub struct {
	canceler sdk.LogisticsShipmentCanceler
	calls    int
}

func (stub *logisticsCancelRuntimeStub) logisticsCanceler(context.Context, tenancy.Scope, sdk.Account, sdk.Runtime) (sdk.LogisticsShipmentCanceler, error) {
	stub.calls++
	return stub.canceler, nil
}

type logisticsCancelConnectorStub struct {
	result sdk.ShipmentResult
	err    error
	calls  int
}

func (stub *logisticsCancelConnectorStub) CancelLogisticsShipment(context.Context, sdk.Account, sdk.Runtime, sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	stub.calls++
	return stub.result, stub.err
}

type logisticsCancelRuntimeValue struct{}

func (logisticsCancelRuntimeValue) Secrets() sdk.SecretAccessor { return nil }

type logisticsCancelApprovalStub struct {
	request  approval.Request
	begin    int
	complete int
	success  []bool
}

func (stub *logisticsCancelApprovalStub) Request(context.Context, tenancy.Scope, string) (approval.Request, error) {
	return stub.request, nil
}

func (stub *logisticsCancelApprovalStub) BeginExecution(_ context.Context, _ tenancy.Scope, command approval.TransitionCommand) (approval.Request, error) {
	started, err := approval.BeginExecution(stub.request, time.Now().UTC())
	if err != nil || command.ExpectedVersion != stub.request.Version {
		return approval.Request{}, err
	}
	stub.begin++
	stub.request = started
	return started, nil
}

func (stub *logisticsCancelApprovalStub) CompleteExecution(_ context.Context, _ tenancy.Scope, command approval.TransitionCommand, success bool) (approval.Request, error) {
	completed, err := approval.CompleteExecution(stub.request, success, command.FailureCode, time.Now().UTC())
	if err != nil {
		return approval.Request{}, err
	}
	stub.complete++
	stub.success = append(stub.success, success)
	stub.request = completed
	return completed, nil
}

func TestLogisticsCancelRouteConsumesApprovalAndCompletesRemoteMutation(t *testing.T) {
	shipment := logisticsCancelShipment()
	remote := &logisticsCancelConnectorStub{result: sdk.ShipmentResult{RemoteID: shipment.RemoteID, Status: "cancelled", Cost: sdk.LogisticsMoney{Currency: "RUB"}, TrackingNumber: shipment.RemoteID, ObservedAt: time.Now().UTC()}}
	approvals := &logisticsCancelApprovalStub{request: logisticsCancelApproval(shipment)}
	shipments := &logisticsCancelShipmentStub{shipment: shipment}
	runtime := &logisticsCancelRuntimeStub{canceler: remote}
	route, err := newLogisticsCancelRoute(shipments, logisticsCancelAccountStub{account: logisticsCancelAccount(t), settings: []sdk.AccountCapabilitySetting{{Capability: "logistics.shipment.cancel", Direction: sdk.CapabilityWrite, Risk: sdk.CapabilityRiskWriteSensitive, ApprovalRequired: true, Enabled: true}}}, approvals, runtime, func(context.Context, tenancy.Scope, sdk.Account) (sdk.Runtime, error) {
		return logisticsCancelRuntimeValue{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := route.Handle(context.Background(), logisticsCancelDelivery(t, shipment, approvals.request.ID)); err != nil {
		t.Fatal(err)
	}
	if approvals.begin != 1 || approvals.complete != 1 || len(approvals.success) != 1 || !approvals.success[0] || remote.calls != 1 || shipments.applied != 1 || runtime.calls != 1 {
		t.Fatalf("approval begin=%d complete=%d success=%v remote=%d applied=%d runtime=%d", approvals.begin, approvals.complete, approvals.success, remote.calls, shipments.applied, runtime.calls)
	}
}

func TestLogisticsCancelRouteFailsPermanentRemoteErrorAndDoesNotRetry(t *testing.T) {
	_ = logisticsCancelScope(t)
	shipment := logisticsCancelShipment()
	connector := &logisticsCancelConnectorStub{err: &sdk.RemoteError{Category: sdk.ErrorInvalidRequest, Code: "cancel_rejected"}}
	approvals := &logisticsCancelApprovalStub{request: logisticsCancelApproval(shipment)}
	shipments := &logisticsCancelShipmentStub{shipment: shipment}
	route, err := newLogisticsCancelRoute(shipments, logisticsCancelAccountStub{account: logisticsCancelAccount(t), settings: []sdk.AccountCapabilitySetting{{Capability: "logistics.shipment.cancel", Direction: sdk.CapabilityWrite, Risk: sdk.CapabilityRiskWriteSensitive, ApprovalRequired: true, Enabled: true}}}, approvals, &logisticsCancelRuntimeStub{canceler: connector}, func(context.Context, tenancy.Scope, sdk.Account) (sdk.Runtime, error) {
		return logisticsCancelRuntimeValue{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = route.Handle(context.Background(), logisticsCancelDelivery(t, shipment, approvals.request.ID))
	class, code := eventbus.ClassifyFailure(err)
	if class != eventbus.FailurePermanent || code != "logistics_cancel_remote_rejected" || approvals.complete != 1 || len(approvals.success) != 1 || approvals.success[0] || shipments.applied != 0 {
		t.Fatalf("class=%v code=%s err=%v complete=%d success=%v applied=%d", class, code, err, approvals.complete, approvals.success, shipments.applied)
	}
}

func TestLogisticsCancelRouteRejectsEventWithoutApprovalReference(t *testing.T) {
	shipment := logisticsCancelShipment()
	approvals := &logisticsCancelApprovalStub{request: logisticsCancelApproval(shipment)}
	route, err := newLogisticsCancelRoute(&logisticsCancelShipmentStub{shipment: shipment}, logisticsCancelAccountStub{account: logisticsCancelAccount(t)}, approvals, &logisticsCancelRuntimeStub{}, func(context.Context, tenancy.Scope, sdk.Account) (sdk.Runtime, error) {
		return logisticsCancelRuntimeValue{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	class, code := eventbus.ClassifyFailure(route.Handle(context.Background(), logisticsCancelDelivery(t, shipment, "")))
	if class != eventbus.FailurePermanent || code != "logistics_cancel_invalid_payload" {
		t.Fatalf("class=%v code=%s", class, code)
	}
}

func TestLogisticsCancelRouteMarksAmbiguousTransportAsUnknown(t *testing.T) {
	shipment := logisticsCancelShipment()
	connector := &logisticsCancelConnectorStub{err: errors.New("transport failed after request")}
	approvals := &logisticsCancelApprovalStub{request: logisticsCancelApproval(shipment)}
	shipments := &logisticsCancelShipmentStub{shipment: shipment}
	route, err := newLogisticsCancelRoute(shipments, logisticsCancelAccountStub{account: logisticsCancelAccount(t), settings: []sdk.AccountCapabilitySetting{{Capability: "logistics.shipment.cancel", Direction: sdk.CapabilityWrite, Risk: sdk.CapabilityRiskWriteSensitive, ApprovalRequired: true, Enabled: true}}}, approvals, &logisticsCancelRuntimeStub{canceler: connector}, func(context.Context, tenancy.Scope, sdk.Account) (sdk.Runtime, error) {
		return logisticsCancelRuntimeValue{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = route.Handle(context.Background(), logisticsCancelDelivery(t, shipment, approvals.request.ID))
	class, code := eventbus.ClassifyFailure(err)
	if class != eventbus.FailurePermanent || code != "logistics_cancel_outcome_unknown" || shipments.unknown != 1 || approvals.complete != 1 || len(approvals.success) != 1 || approvals.success[0] {
		t.Fatalf("class=%v code=%s err=%v unknown=%d complete=%d success=%v", class, code, err, shipments.unknown, approvals.complete, approvals.success)
	}
}

func logisticsCancelDelivery(t *testing.T, shipment logistics.Shipment, approvalID string) eventbus.Delivery {
	t.Helper()
	data := []byte(`{"shipment_id":"` + shipment.ID.String() + `","status":"created","version":1,"operation":"cancel_requested","approval_request_id":"` + approvalID + `"}`)
	return eventbus.Delivery{Event: eventbus.Event{ID: "018f1c8a-7b3c-7def-8000-000000000099", Type: eventbus.EventType(logisticsShipmentChangedEvent), OrganizationID: "018f1c8a-7b3c-7def-8000-000000000001", WorkspaceID: "018f1c8a-7b3c-7def-8000-000000000002", EntityType: "shipment", EntityID: shipment.ID.String(), Source: "api.logistics", CorrelationID: "cancel-key", ActorID: "operator-1", Data: data}, Attempt: 1}
}

func logisticsCancelScope(t *testing.T) tenancy.Scope {
	t.Helper()
	scope, err := tenancy.ParseScope("018f1c8a-7b3c-7def-8000-000000000001", "018f1c8a-7b3c-7def-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func logisticsCancelShipment() logistics.Shipment {
	return logistics.Shipment{ID: "shipment-1", OrganizationID: "018f1c8a-7b3c-7def-8000-000000000001", WorkspaceID: "018f1c8a-7b3c-7def-8000-000000000002", AccountID: "cdek-account", ExternalID: "order-1", RemoteID: "1100285492", ServiceCode: "cdek_tariff_136", Status: logistics.StatusCreated, Currency: "RUB", Version: 1, UpdatedAt: time.Now().UTC()}
}

func logisticsCancelApproval(shipment logistics.Shipment) approval.Request {
	now := time.Now().UTC()
	approvedAt := now.Add(-time.Minute)
	return approval.Request{ID: "018f1c8a-7b3c-7def-8000-000000000010", OrganizationID: shipment.OrganizationID, WorkspaceID: shipment.WorkspaceID, PolicyID: "018f1c8a-7b3c-7def-8000-000000000011", PolicyVersion: 1, RequesterID: "requester-1", Source: "api", Action: "fulfillment.shipment.cancel", ResourceType: "shipment", ResourceID: shipment.ID.String(), CorrelationID: "cancel-key", Risk: approval.RiskWriteSensitive, State: approval.StateApproved, CurrentStage: 1, ExpiresAt: now.Add(time.Hour), Version: 1, RequestedAt: now.Add(-2 * time.Minute), ApprovedAt: &approvedAt}
}

func logisticsCancelAccount(t *testing.T) sdk.Account {
	t.Helper()
	scope := logisticsCancelScope(t)
	now := time.Now().UTC()
	return sdk.Account{ID: "delivery-account", OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), ConnectorID: "carrier", Family: sdk.FamilyLogistics, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: now, UpdatedAt: now}
}
