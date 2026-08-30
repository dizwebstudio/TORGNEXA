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
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

type logisticsCreateShipmentStub struct {
	shipment  logistics.Shipment
	unknown   int
	applied   int
	createRef string
}

func (stub *logisticsCreateShipmentStub) Shipment(context.Context, tenancy.Scope, logistics.ShipmentID) (logistics.Shipment, error) {
	return stub.shipment, nil
}

func (stub *logisticsCreateShipmentStub) CreateRequestReference(context.Context, tenancy.Scope, logistics.ShipmentID) (string, error) {
	return stub.createRef, nil
}

func (stub *logisticsCreateShipmentStub) ApplyCreateResult(_ context.Context, _ tenancy.Scope, _ logistics.ShipmentID, _ int64, result logistics.RemoteResult, _ logistics.Mutation) (logistics.Shipment, error) {
	stub.applied++
	stub.shipment.RemoteID = result.RemoteID
	stub.shipment.Status = result.Status
	stub.shipment.Version++
	return stub.shipment, nil
}

func (stub *logisticsCreateShipmentStub) ApplyCreateUnknown(_ context.Context, _ tenancy.Scope, _ logistics.ShipmentID, _ int64, _ logistics.Mutation) (logistics.Shipment, error) {
	stub.unknown++
	stub.shipment.Status = logistics.StatusUnknown
	stub.shipment.Version++
	return stub.shipment, nil
}

type logisticsCreateRuntimeStub struct{ creator sdk.LogisticsShipmentCreator }

func (stub logisticsCreateRuntimeStub) logisticsCreator(context.Context, tenancy.Scope, sdk.Account, sdk.Runtime) (sdk.LogisticsShipmentCreator, error) {
	return stub.creator, nil
}

type logisticsCreateConnectorStub struct {
	result sdk.ShipmentResult
	err    error
	calls  int
}

func (stub *logisticsCreateConnectorStub) CreateLogisticsShipment(context.Context, sdk.Account, sdk.Runtime, sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	stub.calls++
	return stub.result, stub.err
}

type logisticsCreateSecretStub struct{ payload []byte }

func (stub logisticsCreateSecretStub) Create(context.Context, tenancy.Scope, secrets.Class, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, nil
}
func (stub logisticsCreateSecretStub) Use(_ context.Context, _ tenancy.Scope, _ secrets.Reference, consumer func([]byte) error) error {
	return consumer(stub.payload)
}
func (stub logisticsCreateSecretStub) Describe(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	return secrets.Metadata{}, nil
}
func (stub logisticsCreateSecretStub) Rotate(context.Context, tenancy.Scope, secrets.Reference, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, nil
}
func (stub logisticsCreateSecretStub) Revoke(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	return secrets.Metadata{}, nil
}

func TestLogisticsCreateRouteExecutesApprovedCDEKShipmentOnce(t *testing.T) {
	shipment := logisticsCreateShipment()
	approvalRequest := logisticsCreateApproval(shipment)
	payload := []byte(`{"external_id":"order-17","service_code":"cdek_tariff_136","idempotency_key":"create-key-1","from":{"country":"RU","city":"Москва","line1":"Тверская, 1"},"to":{"country":"RU","city":"Санкт-Петербург","line1":"Невский, 1"},"parcels":[{"weight_grams":1000,"length_mm":100,"width_mm":100,"height_mm":100}],"sender":{"name":"ООО Торгнекса","phone":"+74951234567"},"recipient":{"name":"Иван Петров","phone":"+79991234567"}}`)
	connector := &logisticsCreateConnectorStub{result: sdk.ShipmentResult{RemoteID: "1100285492", Status: "created", Cost: sdk.LogisticsMoney{Currency: "RUB"}, TrackingNumber: "1100285492", ObservedAt: time.Now().UTC()}}
	shipments := &logisticsCreateShipmentStub{shipment: shipment, createRef: "sec:v1:0123456789abcdef0123456789abcdef"}
	approvals := &logisticsCancelApprovalStub{request: approvalRequest}
	route, err := newLogisticsCreateRoute(shipments, logisticsCancelAccountStub{account: logisticsCreateAccount(t), settings: []sdk.AccountCapabilitySetting{{Capability: "logistics.shipment.create", Direction: sdk.CapabilityWrite, Risk: sdk.CapabilityRiskWriteSensitive, ApprovalRequired: true, Enabled: true}}}, approvals, logisticsCreateRuntimeStub{creator: connector}, logisticsCreateSecretStub{payload: payload}, func(context.Context, tenancy.Scope, sdk.Account) (sdk.Runtime, error) {
		return logisticsCancelRuntimeValue{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := route.Handle(context.Background(), logisticsCreateDelivery(t, shipment, approvalRequest.ID)); err != nil {
		t.Fatal(err)
	}
	if connector.calls != 1 || shipments.applied != 1 || shipments.unknown != 0 || approvals.begin != 1 || approvals.complete != 1 || len(approvals.success) != 1 || !approvals.success[0] {
		t.Fatalf("remote=%d applied=%d unknown=%d begin=%d complete=%d success=%v", connector.calls, shipments.applied, shipments.unknown, approvals.begin, approvals.complete, approvals.success)
	}
}

func TestLogisticsCreateRouteMarksRemoteOutcomeUnknownWithoutRetry(t *testing.T) {
	shipment := logisticsCreateShipment()
	approvalRequest := logisticsCreateApproval(shipment)
	connector := &logisticsCreateConnectorStub{err: errors.New("transport failed after request")}
	shipments := &logisticsCreateShipmentStub{shipment: shipment, createRef: "sec:v1:0123456789abcdef0123456789abcdef"}
	approvals := &logisticsCancelApprovalStub{request: approvalRequest}
	payload := []byte(`{"external_id":"order-17","service_code":"cdek_tariff_136","idempotency_key":"create-key-1","from":{"country":"RU","city":"Москва","line1":"Тверская, 1"},"to":{"country":"RU","city":"Санкт-Петербург","line1":"Невский, 1"},"parcels":[{"weight_grams":1000,"length_mm":100,"width_mm":100,"height_mm":100}],"sender":{"name":"ООО Торгнекса","phone":"+74951234567"},"recipient":{"name":"Иван Петров","phone":"+79991234567"}}`)
	route, err := newLogisticsCreateRoute(shipments, logisticsCancelAccountStub{account: logisticsCreateAccount(t), settings: []sdk.AccountCapabilitySetting{{Capability: "logistics.shipment.create", Direction: sdk.CapabilityWrite, Risk: sdk.CapabilityRiskWriteSensitive, ApprovalRequired: true, Enabled: true}}}, approvals, logisticsCreateRuntimeStub{creator: connector}, logisticsCreateSecretStub{payload: payload}, func(context.Context, tenancy.Scope, sdk.Account) (sdk.Runtime, error) {
		return logisticsCancelRuntimeValue{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	class, code := eventbus.ClassifyFailure(route.Handle(context.Background(), logisticsCreateDelivery(t, shipment, approvalRequest.ID)))
	if class != eventbus.FailurePermanent || code != "logistics_create_outcome_unknown" || connector.calls != 1 || shipments.unknown != 1 || shipments.applied != 0 || approvals.complete != 1 || len(approvals.success) != 1 || approvals.success[0] {
		t.Fatalf("class=%v code=%s remote=%d unknown=%d applied=%d complete=%d success=%v", class, code, connector.calls, shipments.unknown, shipments.applied, approvals.complete, approvals.success)
	}
}

func logisticsCreateDelivery(t *testing.T, shipment logistics.Shipment, approvalID string) eventbus.Delivery {
	t.Helper()
	data := []byte(`{"shipment_id":"` + shipment.ID.String() + `","status":"pending","version":1,"operation":"create_requested","approval_request_id":"` + approvalID + `"}`)
	return eventbus.Delivery{Event: eventbus.Event{ID: "018f1c8a-7b3c-7def-8000-000000000199", Type: eventbus.EventType(logisticsShipmentChangedEvent), OrganizationID: shipment.OrganizationID, WorkspaceID: shipment.WorkspaceID, EntityType: "shipment", EntityID: shipment.ID.String(), Source: "api.logistics", CorrelationID: "create-key-1", ActorID: "operator-1", Data: data}, Attempt: 1}
}

func logisticsCreateShipment() logistics.Shipment {
	return logistics.Shipment{ID: "shipment-create-1", OrganizationID: "018f1c8a-7b3c-7def-8000-000000000001", WorkspaceID: "018f1c8a-7b3c-7def-8000-000000000002", AccountID: "cdek-account", ExternalID: "order-17", ServiceCode: "cdek_tariff_136", Status: logistics.StatusPending, Currency: "RUB", Version: 1, UpdatedAt: time.Now().UTC()}
}

func logisticsCreateApproval(shipment logistics.Shipment) approval.Request {
	now := time.Now().UTC()
	approvedAt := now.Add(-time.Minute)
	return approval.Request{ID: "018f1c8a-7b3c-7def-8000-000000000010", OrganizationID: shipment.OrganizationID, WorkspaceID: shipment.WorkspaceID, PolicyID: "018f1c8a-7b3c-7def-8000-000000000011", PolicyVersion: 1, RequesterID: "requester-1", Source: "api", Action: "fulfillment.shipment.create", ResourceType: "shipment", ResourceID: shipment.ID.String(), CorrelationID: "create-key-1", Risk: approval.RiskWriteSensitive, State: approval.StateApproved, CurrentStage: 1, ExpiresAt: now.Add(time.Hour), Version: 1, RequestedAt: now.Add(-2 * time.Minute), ApprovedAt: &approvedAt}
}

func logisticsCreateAccount(t *testing.T) sdk.Account {
	account := logisticsCancelAccount(t)
	account.ID = "cdek-account"
	account.ConnectorID = "cdek"
	return account
}
