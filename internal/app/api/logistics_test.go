package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
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
