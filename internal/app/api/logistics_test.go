package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/logistics"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

type logisticsAccountStub struct {
	account  sdk.Account
	settings []sdk.AccountCapabilitySetting
}

func (stub logisticsAccountStub) AccountByID(context.Context, string, string, string) (sdk.Account, error) {
	return stub.account, nil
}

func (stub logisticsAccountStub) AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error) {
	return stub.settings, nil
}

type logisticsRuntimeStub struct {
	supported bool
	points    []sdk.PickupPoint
	rates     []sdk.RateQuote
	tracking  sdk.ShipmentResult
	label     sdk.LabelResult
}

type logisticsBatchRuntimeStub struct {
	logisticsRuntimeStub
	batches []sdk.LogisticsBatch
}

type logisticsShipmentStub struct {
	shipment logistics.Shipment
	fresh    bool
	called   bool
	mutation logistics.Mutation
}

func (stub *logisticsShipmentStub) BeginCreate(_ context.Context, _ tenancy.Scope, command logistics.CreateCommand, mutation logistics.Mutation) (logistics.Shipment, bool, error) {
	stub.called = true
	stub.mutation = mutation
	stub.shipment.ID = command.ID
	stub.shipment.AccountID = command.AccountID
	stub.shipment.ExternalID = command.ExternalID
	stub.shipment.ServiceCode = command.ServiceCode
	return stub.shipment, stub.fresh, nil
}

func (stub *logisticsShipmentStub) BeginCancel(_ context.Context, _ tenancy.Scope, _ logistics.ShipmentID, _ string, mutation logistics.Mutation) (logistics.Shipment, bool, error) {
	stub.called = true
	stub.mutation = mutation
	return stub.shipment, stub.fresh, nil
}

type logisticsApprovalStub struct {
	request approval.Request
	err     error
}

func (stub logisticsApprovalStub) Request(context.Context, tenancy.Scope, string) (approval.Request, error) {
	return stub.request, stub.err
}

func (stub logisticsRuntimeStub) SupportsCapability(string, string) bool { return stub.supported }

func (stub logisticsRuntimeStub) PickupPoints(_ context.Context, _ sdk.Account, runtime sdk.Runtime, query sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	if runtime == nil || query.Limit < 1 {
		return nil, errors.New("runtime or query missing")
	}
	return stub.points, nil
}

func (stub logisticsRuntimeStub) LogisticsRates(_ context.Context, _ sdk.Account, runtime sdk.Runtime, query sdk.RateRequest) ([]sdk.RateQuote, error) {
	if runtime == nil || query.Validate() != nil {
		return nil, errors.New("runtime or request missing")
	}
	return stub.rates, nil
}

func (stub logisticsRuntimeStub) LogisticsTracking(_ context.Context, _ sdk.Account, runtime sdk.Runtime, query sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	if runtime == nil || query.RemoteID == "" {
		return sdk.ShipmentResult{}, errors.New("runtime or tracking request missing")
	}
	return stub.tracking, nil
}

func (stub logisticsRuntimeStub) LogisticsLabel(_ context.Context, _ sdk.Account, runtime sdk.Runtime, query sdk.LabelRequest) (sdk.LabelResult, error) {
	if runtime == nil || query.Validate() != nil {
		return sdk.LabelResult{}, errors.New("runtime or label request missing")
	}
	return stub.label, nil
}

func (stub logisticsBatchRuntimeStub) LogisticsBatches(_ context.Context, _ sdk.Account, runtime sdk.Runtime, query sdk.LogisticsBatchQuery) ([]sdk.LogisticsBatch, error) {
	if runtime == nil || query.Validate(100) != nil {
		return nil, errors.New("runtime or batch query missing")
	}
	return stub.batches, nil
}

type logisticsSecretsStub struct{}

func (logisticsSecretsStub) Create(context.Context, tenancy.Scope, secrets.Class, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, nil
}
func (logisticsSecretsStub) Use(_ context.Context, _ tenancy.Scope, _ secrets.Reference, consumer func([]byte) error) error {
	return consumer([]byte("synthetic"))
}
func (logisticsSecretsStub) Describe(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	return secrets.Metadata{}, nil
}
func (logisticsSecretsStub) Rotate(context.Context, tenancy.Scope, secrets.Reference, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, nil
}
func (logisticsSecretsStub) Revoke(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	return secrets.Metadata{}, nil
}

func logisticsTestAccount(t *testing.T) sdk.Account {
	t.Helper()
	scope := validTestScope(t)
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "cdek-account", OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), ConnectorID: "c" + "dek", Family: sdk.FamilyLogistics, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: now, UpdatedAt: now}
}

func logisticsRequest(t *testing.T, scope tenancy.Scope, target string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	return request.WithContext(context.WithValue(request.Context(), requestScopeKey{}, scope))
}

func TestLogisticsPickupPointsRouteRequiresEnabledCapabilityAndReturnsCanonicalPage(t *testing.T) {
	scope := validTestScope(t)
	account := logisticsTestAccount(t)
	settings := []sdk.AccountCapabilitySetting{{Capability: "pickup.points.read", Direction: sdk.CapabilityRead, Risk: sdk.CapabilityRiskRead, Enabled: true}}
	route := newLogisticsRoutes(logisticsAccountStub{account: account, settings: settings}, logisticsSecretsStub{}, logisticsRuntimeStub{supported: true, points: []sdk.PickupPoint{{RemoteID: "office-1", Name: "ПВЗ", Country: "RU", City: "Москва", Address: "Тверская, 1", Active: true}}})[0]
	request := logisticsRequest(t, scope, logisticsPickupPointsPath+"?connector_account_id=cdek-account&country=ru&city=%D0%9C%D0%BE%D1%81%D0%BA%D0%B2%D0%B0&limit=10")
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"remote_id":"office-1"`) || !strings.Contains(response.Body.String(), `"country":"RU"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLogisticsPickupPointsRouteFailsClosedWithoutCapability(t *testing.T) {
	scope := validTestScope(t)
	account := logisticsTestAccount(t)
	route := newLogisticsRoutes(logisticsAccountStub{account: account}, logisticsSecretsStub{}, logisticsRuntimeStub{supported: false})[0]
	request := logisticsRequest(t, scope, logisticsPickupPointsPath+"?connector_account_id=cdek-account&country=RU&city=%D0%9C%D0%BE%D1%81%D0%BA%D0%B2%D0%B0")
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLogisticsBatchesRouteReturnsBoundedPage(t *testing.T) {
	scope := validTestScope(t)
	account := logisticsTestAccount(t)
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	settings := []sdk.AccountCapabilitySetting{{Capability: "logistics.batches.read", Direction: sdk.CapabilityRead, Risk: sdk.CapabilityRiskRead, Enabled: true}}
	routes := newLogisticsRoutes(logisticsAccountStub{account: account, settings: settings}, logisticsSecretsStub{}, logisticsBatchRuntimeStub{
		logisticsRuntimeStub: logisticsRuntimeStub{supported: true},
		batches:              []sdk.LogisticsBatch{{RemoteID: "batch-1", Status: "CREATED", ShipmentCount: 2, ObservedAt: now}},
	})
	var route ProtectedRoute
	for _, candidate := range routes {
		if candidate.Path == logisticsBatchesPath {
			route = candidate
		}
	}
	if route.Handler == nil {
		t.Fatal("batch route not registered")
	}
	request := logisticsRequest(t, scope, logisticsBatchesPath+"?connector_account_id=cdek-account&mail_type=ONLINE_PARCEL&limit=10&page=2")
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"remote_id":"batch-1"`) || !strings.Contains(response.Body.String(), `"shipment_count":2`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLogisticsRatesRouteReturnsNeutralPreviewOptions(t *testing.T) {
	scope := validTestScope(t)
	account := logisticsTestAccount(t)
	settings := []sdk.AccountCapabilitySetting{{Capability: "logistics.rates.read", Direction: sdk.CapabilityRead, Risk: sdk.CapabilityRiskRead, Enabled: true}}
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	routes := newLogisticsRoutes(logisticsAccountStub{account: account, settings: settings}, logisticsSecretsStub{}, logisticsRuntimeStub{supported: true, rates: []sdk.RateQuote{{ServiceCode: "cdek_tariff_136", Cost: sdk.LogisticsMoney{MinorUnits: 12345, Currency: "RUB"}, MinDeliveryAt: now.Add(24 * time.Hour), MaxDeliveryAt: now.Add(48 * time.Hour), ObservedAt: now}}})
	var route ProtectedRoute
	for _, candidate := range routes {
		if candidate.Path == logisticsRatesPath {
			route = candidate
		}
	}
	if route.Handler == nil {
		t.Fatal("rates route not registered")
	}
	body := strings.NewReader(`{"connector_account_id":"cdek-account","from":{"country":"RU","postal_code":"101000","city":"Москва","line1":"Тверская, 1"},"to":{"country":"RU","postal_code":"190000","city":"Санкт-Петербург","line1":"Невский, 1"},"parcels":[{"weight_grams":1000,"length_mm":100,"width_mm":100,"height_mm":100}]}`)
	request := httptest.NewRequest(http.MethodPost, logisticsRatesPath, body).WithContext(context.WithValue(context.Background(), requestScopeKey{}, scope))
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"option_id":"option-`) || strings.Contains(response.Body.String(), "cdek_tariff_136") || !strings.Contains(response.Body.String(), `"minor_units":12345`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLogisticsRatesRouteFailsClosedWithoutEnabledCapability(t *testing.T) {
	scope := validTestScope(t)
	account := logisticsTestAccount(t)
	routes := newLogisticsRoutes(logisticsAccountStub{account: account}, logisticsSecretsStub{}, logisticsRuntimeStub{supported: true})
	var route ProtectedRoute
	for _, candidate := range routes {
		if candidate.Path == logisticsRatesPath {
			route = candidate
		}
	}
	body := strings.NewReader(`{"connector_account_id":"cdek-account","from":{"country":"RU","city":"Москва","line1":"Тверская, 1"},"to":{"country":"RU","city":"Москва","line1":"Тверская, 2"},"parcels":[{"weight_grams":1000,"length_mm":100,"width_mm":100,"height_mm":100}]}`)
	request := httptest.NewRequest(http.MethodPost, logisticsRatesPath, body).WithContext(context.WithValue(context.Background(), requestScopeKey{}, scope))
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLogisticsTrackingRouteReturnsNeutralStatus(t *testing.T) {
	scope := validTestScope(t)
	account := logisticsTestAccount(t)
	settings := []sdk.AccountCapabilitySetting{{Capability: "logistics.track.read", Direction: sdk.CapabilityRead, Risk: sdk.CapabilityRiskRead, Enabled: true}}
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	routes := newLogisticsRoutes(logisticsAccountStub{account: account, settings: settings}, logisticsSecretsStub{}, logisticsRuntimeStub{supported: true, tracking: sdk.ShipmentResult{RemoteID: "1100285492", Status: "DELIVERED", TrackingNumber: "1100285492", ObservedAt: now}})
	var route ProtectedRoute
	for _, candidate := range routes {
		if candidate.Path == logisticsTrackingPath {
			route = candidate
		}
	}
	if route.Handler == nil {
		t.Fatal("tracking route not registered")
	}
	request := logisticsRequest(t, scope, logisticsTrackingPath+"?connector_account_id=cdek-account&remote_id=1100285492")
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"remote_id":"1100285492"`) || !strings.Contains(response.Body.String(), `"status":"DELIVERED"`) || !strings.Contains(response.Body.String(), `"tracking_number":"1100285492"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLogisticsTrackingRouteFailsClosedWithoutEnabledCapability(t *testing.T) {
	scope := validTestScope(t)
	account := logisticsTestAccount(t)
	routes := newLogisticsRoutes(logisticsAccountStub{account: account}, logisticsSecretsStub{}, logisticsRuntimeStub{supported: true})
	var route ProtectedRoute
	for _, candidate := range routes {
		if candidate.Path == logisticsTrackingPath {
			route = candidate
		}
	}
	request := logisticsRequest(t, scope, logisticsTrackingPath+"?connector_account_id=cdek-account&remote_id=1100285492")
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLogisticsLabelRouteReturnsArtifactReference(t *testing.T) {
	scope := validTestScope(t)
	account := logisticsTestAccount(t)
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	settings := []sdk.AccountCapabilitySetting{{Capability: "logistics.label.read", Direction: sdk.CapabilityRead, Risk: sdk.CapabilityRiskRead, Enabled: true}}
	routes := newLogisticsRoutes(logisticsAccountStub{account: account, settings: settings}, logisticsSecretsStub{}, logisticsRuntimeStub{supported: true, label: sdk.LabelResult{ArtifactRef: "cdek:print:barcode:72753031-1820-4f99-9240-aab139f05ca5", MediaType: "application/pdf", ObservedAt: now}})
	var route ProtectedRoute
	for _, candidate := range routes {
		if candidate.Path == logisticsLabelsPath {
			route = candidate
		}
	}
	if route.Handler == nil {
		t.Fatal("label route not registered")
	}
	request := logisticsRequest(t, scope, logisticsLabelsPath+"?connector_account_id=cdek-account&remote_id=1100285492&format=pdf")
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"artifact_ref":"cdek:print:barcode:`) || !strings.Contains(response.Body.String(), `"media_type":"application/pdf"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLogisticsLabelRouteFailsClosedWithoutEnabledCapability(t *testing.T) {
	scope := validTestScope(t)
	account := logisticsTestAccount(t)
	routes := newLogisticsRoutes(logisticsAccountStub{account: account}, logisticsSecretsStub{}, logisticsRuntimeStub{supported: true})
	var route ProtectedRoute
	for _, candidate := range routes {
		if candidate.Path == logisticsLabelsPath {
			route = candidate
		}
	}
	request := logisticsRequest(t, scope, logisticsLabelsPath+"?connector_account_id=cdek-account&remote_id=1100285492")
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLogisticsCancellationRouteRequiresMatchingApprovalAndPersistsApprovalReference(t *testing.T) {
	scope := validTestScope(t)
	principal := Principal{Issuer: "https://id.example.test", Subject: "operator-1"}
	shipmentID := logistics.ShipmentID("shipment-1")
	shipment := logistics.Shipment{ID: shipmentID, Status: logistics.StatusCreated, Version: 1}
	store := &logisticsShipmentStub{shipment: shipment, fresh: true}
	approvalID := "approval-1"
	routes := newLogisticsRoutes(nil, nil, nil, logisticsRouteDependency{
		shipments: store,
		approvals: logisticsApprovalStub{request: approval.Request{ID: approvalID, Action: "fulfillment.shipment.cancel", ResourceType: "shipment", ResourceID: shipmentID.String(), Risk: approval.RiskWriteSensitive, State: approval.StateApproved}},
	})
	var route ProtectedRoute
	for _, candidate := range routes {
		if candidate.Path == logisticsShipmentsPath {
			route = candidate
		}
	}
	request := httptest.NewRequest(http.MethodPost, logisticsShipmentsPath+shipmentID.String()+"/cancel", nil)
	request.Header.Set("Idempotency-Key", "cancel-key")
	request.Header.Set("Approval-Request-ID", approvalID)
	ctx := context.WithValue(request.Context(), requestScopeKey{}, scope)
	ctx = context.WithValue(ctx, requestIdentityKey{}, principal)
	request = request.WithContext(ctx)
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !store.called || store.mutation.ApprovalRequestID != approvalID || !strings.Contains(response.Body.String(), `"accepted":true`) {
		t.Fatalf("status=%d body=%s called=%v mutation=%+v", response.Code, response.Body.String(), store.called, store.mutation)
	}
}

func TestLogisticsShipmentCreationRouteRequiresApprovalAndQueuesEncryptedPayload(t *testing.T) {
	scope := validTestScope(t)
	principal := Principal{Issuer: "https://id.example.test", Subject: "operator-1"}
	shipmentID := logistics.ShipmentID("shipment-create-1")
	store := &logisticsShipmentStub{fresh: true}
	approvalID := "approval-create-1"
	routes := newLogisticsRoutes(logisticsAccountStub{account: logisticsTestAccount(t), settings: []sdk.AccountCapabilitySetting{{Capability: "logistics.shipment.create", Direction: sdk.CapabilityWrite, Risk: sdk.CapabilityRiskWriteSensitive, ApprovalRequired: true, Enabled: true}}}, logisticsSecretsStub{}, logisticsRuntimeStub{supported: true}, logisticsRouteDependency{shipments: store, approvals: logisticsApprovalStub{request: approval.Request{ID: approvalID, Action: "fulfillment.shipment.create", ResourceType: "shipment", ResourceID: shipmentID.String(), Risk: approval.RiskWriteSensitive, State: approval.StateApproved}}})
	var route ProtectedRoute
	for _, candidate := range routes {
		if candidate.Path == logisticsShipmentCreatePath {
			route = candidate
		}
	}
	body := strings.NewReader(`{"shipment_id":"shipment-create-1","connector_account_id":"cdek-account","external_id":"order-17","service_code":"cdek_tariff_136","from":{"country":"RU","postal_code":"101000","city":"Москва","line1":"Тверская, 1"},"to":{"country":"RU","postal_code":"190000","city":"Санкт-Петербург","line1":"Невский, 1"},"parcels":[{"weight_grams":1000,"length_mm":100,"width_mm":100,"height_mm":100}],"pickup_point_ref":"pvz-137","sender":{"name":"ООО Торгнекса","phone":"+74951234567"},"recipient":{"name":"Иван Петров","phone":"+79991234567"}}`)
	request := httptest.NewRequest(http.MethodPost, logisticsShipmentCreatePath, body)
	request.Header.Set("Idempotency-Key", "create-key-1")
	request.Header.Set("Approval-Request-ID", approvalID)
	ctx := context.WithValue(request.Context(), requestScopeKey{}, scope)
	ctx = context.WithValue(ctx, requestIdentityKey{}, principal)
	request = request.WithContext(ctx)
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !store.called || store.mutation.ApprovalRequestID != approvalID || !strings.Contains(response.Body.String(), `"accepted":true`) {
		t.Fatalf("status=%d body=%s called=%v mutation=%+v", response.Code, response.Body.String(), store.called, store.mutation)
	}
}

func TestLogisticsCancellationRouteRejectsNonApprovedRequest(t *testing.T) {
	scope := validTestScope(t)
	shipmentID := logistics.ShipmentID("shipment-1")
	store := &logisticsShipmentStub{shipment: logistics.Shipment{ID: shipmentID, Status: logistics.StatusCreated, Version: 1}}
	routes := newLogisticsRoutes(nil, nil, nil, logisticsRouteDependency{shipments: store, approvals: logisticsApprovalStub{request: approval.Request{ID: "approval-1", Action: "fulfillment.shipment.cancel", ResourceType: "shipment", ResourceID: shipmentID.String(), Risk: approval.RiskWriteSensitive, State: approval.StatePending}}})
	var route ProtectedRoute
	for _, candidate := range routes {
		if candidate.Path == logisticsShipmentsPath {
			route = candidate
		}
	}
	request := httptest.NewRequest(http.MethodPost, logisticsShipmentsPath+shipmentID.String()+"/cancel", nil)
	request.Header.Set("Idempotency-Key", "cancel-key")
	request.Header.Set("Approval-Request-ID", "approval-1")
	request = request.WithContext(context.WithValue(context.WithValue(request.Context(), requestScopeKey{}, scope), requestIdentityKey{}, Principal{Issuer: "issuer", Subject: "operator-1"}))
	response := httptest.NewRecorder()
	route.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || store.called {
		t.Fatalf("status=%d body=%s called=%v", response.Code, response.Body.String(), store.called)
	}
}
