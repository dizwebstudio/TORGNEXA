package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	core "github.com/torgnexa/torgnexa/internal/core/returns"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

type returnLogisticsStoreStub struct {
	core.Repository
	operation core.ReturnLogisticsOperation
	begin     int
	result    int
	failure   int
	unknown   int
}

func (stub *returnLogisticsStoreStub) ReturnLogistics(context.Context, core.Scope, core.ReturnLogisticsOperationID) (core.ReturnLogisticsOperation, error) {
	return stub.operation, nil
}

func (stub *returnLogisticsStoreStub) BeginReturnLogistics(_ context.Context, _ core.Scope, _ core.ReturnLogisticsOperationID, _ core.Mutation) (core.ReturnLogisticsOperation, bool, error) {
	if stub.operation.Status != core.ReturnLogisticsRequested {
		return stub.operation, false, nil
	}
	stub.begin++
	stub.operation.Status = core.ReturnLogisticsExecuting
	stub.operation.Version++
	return stub.operation, true, nil
}

func (stub *returnLogisticsStoreStub) ApplyReturnLogisticsResult(_ context.Context, _ core.Scope, _ core.ReturnLogisticsOperationID, _ int64, result core.ReturnLogisticsResult, _ core.Mutation) (core.ReturnLogisticsOperation, error) {
	stub.result++
	stub.operation.Status = core.ReturnLogisticsSucceeded
	stub.operation.RemoteID = result.RemoteID
	stub.operation.TrackingNumber = result.TrackingNumber
	stub.operation.Version++
	return stub.operation, nil
}

func (stub *returnLogisticsStoreStub) ApplyReturnLogisticsFailure(_ context.Context, _ core.Scope, _ core.ReturnLogisticsOperationID, _ int64, failureCode string, _ core.Mutation) (core.ReturnLogisticsOperation, error) {
	stub.failure++
	stub.operation.Status = core.ReturnLogisticsFailed
	stub.operation.FailureCode = failureCode
	stub.operation.Version++
	return stub.operation, nil
}

func (stub *returnLogisticsStoreStub) ApplyReturnLogisticsUnknown(_ context.Context, _ core.Scope, _ core.ReturnLogisticsOperationID, _ int64, _ core.Mutation) (core.ReturnLogisticsOperation, error) {
	stub.unknown++
	stub.operation.Status = core.ReturnLogisticsUnknown
	stub.operation.Version++
	return stub.operation, nil
}

type returnLogisticsRuntimeStub struct {
	creator sdk.LogisticsReturnCreator
	calls   int
}

func (stub *returnLogisticsRuntimeStub) logisticsReturnCreator(context.Context, tenancy.Scope, sdk.Account, sdk.Runtime) (sdk.LogisticsReturnCreator, error) {
	stub.calls++
	return stub.creator, nil
}

type returnLogisticsCreatorStub struct {
	result sdk.ShipmentResult
	err    error
	calls  int
}

func (stub *returnLogisticsCreatorStub) CreateLogisticsReturn(context.Context, sdk.Account, sdk.Runtime, sdk.ReturnCreateRequest) (sdk.ShipmentResult, error) {
	stub.calls++
	return stub.result, stub.err
}

func TestReturnLogisticsRoutePersistsSuccessAndDoesNotRepeatRemoteCall(t *testing.T) {
	operation := returnLogisticsOperationForWorker()
	store := &returnLogisticsStoreStub{operation: operation}
	creator := &returnLogisticsCreatorStub{result: sdk.ShipmentResult{RemoteID: "RA644000002RU", Status: "created", TrackingNumber: "RA644000002RU", ObservedAt: time.Now().UTC()}}
	runtime := &returnLogisticsRuntimeStub{creator: creator}
	route := returnLogisticsRouteForTest(t, store, runtime)
	delivery := returnLogisticsDelivery(t, operation)

	if err := route.Handle(context.Background(), delivery); err != nil {
		t.Fatal(err)
	}
	if err := route.Handle(context.Background(), delivery); err != nil {
		t.Fatal(err)
	}
	if store.begin != 1 || store.result != 1 || store.operation.Status != core.ReturnLogisticsSucceeded || creator.calls != 1 || runtime.calls != 1 {
		t.Fatalf("begin=%d result=%d status=%s creator=%d runtime=%d", store.begin, store.result, store.operation.Status, creator.calls, runtime.calls)
	}
}

func TestReturnLogisticsRouteRecordsPermanentProviderRejection(t *testing.T) {
	operation := returnLogisticsOperationForWorker()
	store := &returnLogisticsStoreStub{operation: operation}
	creator := &returnLogisticsCreatorStub{err: &sdk.RemoteError{Category: sdk.ErrorInvalidRequest, Code: "return_rejected"}}
	route := returnLogisticsRouteForTest(t, store, &returnLogisticsRuntimeStub{creator: creator})

	class, code := eventbus.ClassifyFailure(route.Handle(context.Background(), returnLogisticsDelivery(t, operation)))
	if class != eventbus.FailurePermanent || code != "return_logistics_remote_rejected" || store.failure != 1 || store.operation.Status != core.ReturnLogisticsFailed || creator.calls != 1 {
		t.Fatalf("class=%v code=%s failure=%d status=%s creator=%d", class, code, store.failure, store.operation.Status, creator.calls)
	}
}

func TestReturnLogisticsRouteMarksAmbiguousProviderOutcomeUnknown(t *testing.T) {
	operation := returnLogisticsOperationForWorker()
	store := &returnLogisticsStoreStub{operation: operation}
	route := returnLogisticsRouteForTest(t, store, &returnLogisticsRuntimeStub{creator: &returnLogisticsCreatorStub{err: errors.New("connection closed after request")}})

	class, code := eventbus.ClassifyFailure(route.Handle(context.Background(), returnLogisticsDelivery(t, operation)))
	if class != eventbus.FailurePermanent || code != "return_logistics_outcome_unknown" || store.unknown != 1 || store.operation.Status != core.ReturnLogisticsUnknown {
		t.Fatalf("class=%v code=%s unknown=%d status=%s", class, code, store.unknown, store.operation.Status)
	}
}

func returnLogisticsRouteForTest(t *testing.T, store *returnLogisticsStoreStub, runtime *returnLogisticsRuntimeStub) *returnLogisticsRoute {
	t.Helper()
	scope, err := tenancy.ParseScope("018f1c8a-7b3c-7def-8000-000000000001", "018f1c8a-7b3c-7def-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	account := sdk.Account{ID: "logistics-account", OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), ConnectorID: "logistics-a", Family: sdk.FamilyLogistics, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: now, UpdatedAt: now}
	accounts := logisticsCancelAccountStub{account: account, settings: []sdk.AccountCapabilitySetting{{Capability: "logistics.return.create", Direction: sdk.CapabilityWrite, Risk: sdk.CapabilityRiskWriteSensitive, ApprovalRequired: true, Enabled: true}}}
	route, err := newReturnLogisticsRoute(store, accounts, runtime, func(context.Context, tenancy.Scope, sdk.Account) (sdk.Runtime, error) {
		return logisticsCancelRuntimeValue{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return route
}

func returnLogisticsOperationForWorker() core.ReturnLogisticsOperation {
	now := time.Now().UTC()
	return core.ReturnLogisticsOperation{
		ID:                 "018f1c8a-7b3c-7def-8000-000000000010",
		OrganizationID:     "018f1c8a-7b3c-7def-8000-000000000001",
		WorkspaceID:        "018f1c8a-7b3c-7def-8000-000000000002",
		ReturnID:           "018f1c8a-7b3c-7def-8000-000000000011",
		ConnectorAccountID: "logistics-account",
		OriginalRemoteID:   "RA644000001RU",
		ExternalID:         "return-001",
		MailType:           "POSTAL_PARCEL",
		Status:             core.ReturnLogisticsRequested,
		Version:            1,
		IdempotencyKey:     "return-logistics-key",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func returnLogisticsDelivery(t *testing.T, operation core.ReturnLogisticsOperation) eventbus.Delivery {
	t.Helper()
	data := []byte(`{"operation_id":"` + operation.ID.String() + `","return_id":"` + operation.ReturnID.String() + `","version":1}`)
	return eventbus.Delivery{Event: eventbus.Event{ID: "018f1c8a-7b3c-7def-8000-000000000099", Type: eventbus.EventType(returnLogisticsRequestedEvent), OrganizationID: operation.OrganizationID, WorkspaceID: operation.WorkspaceID, EntityType: "return_logistics", EntityID: operation.ID.String(), Source: "api.returns", CorrelationID: "return-logistics-key", ActorID: "operator-1", Data: data}, Attempt: 1}
}
