package pek

import (
	"context"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type testRuntime struct{}

func (testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{} }

type testSecrets struct{}

func (testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	return callback([]byte(`{"username":"synthetic-user","password":"synthetic-key"}`))
}

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "pek-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "pek", Family: sdk.FamilyLogistics, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

func TestRatesShipmentTrackingAndPickup(t *testing.T) {
	connector := New(candidateTransport{}, nil)
	address := sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Примерная ул., 1"}
	parcel := sdk.Parcel{WeightGrams: 1000, LengthMM: 100, WidthMM: 100, HeightMM: 100}
	rates, err := connector.ReadLogisticsRates(context.Background(), testAccount(), testRuntime{}, sdk.RateRequest{From: address, To: address, Parcels: []sdk.Parcel{parcel}})
	if err != nil || len(rates) != 1 {
		t.Fatalf("rates=%+v err=%v", rates, err)
	}
	shipment, err := connector.CreateLogisticsShipment(context.Background(), testAccount(), testRuntime{}, sdk.ShipmentCreateRequest{ExternalID: "order:pek:1", ServiceCode: "road", IdempotencyKey: "idem:pek:1", From: address, To: address, Parcels: []sdk.Parcel{parcel}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = connector.ReadLogisticsTracking(context.Background(), testAccount(), testRuntime{}, sdk.ShipmentStatusRequest{RemoteID: shipment.RemoteID}); err != nil {
		t.Fatal(err)
	}
	if _, err = connector.ReadPickupPoints(context.Background(), testAccount(), testRuntime{}, sdk.PickupPointQuery{Country: "RU", City: "Москва", Limit: 10}); err != nil {
		t.Fatal(err)
	}
}

func TestHealthRejectsMissingRuntime(t *testing.T) {
	if _, err := New(candidateTransport{}, nil).Health(context.Background(), testAccount(), nil); err == nil {
		t.Fatal("expected missing runtime to be rejected")
	}
}
