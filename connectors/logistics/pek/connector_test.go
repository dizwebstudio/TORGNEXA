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

type testConfigurationSource struct{}

func (testConfigurationSource) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{SenderWarehouseID: "abcd1234-0001", SenderLegalForm: 1, SenderTitle: "ООО Пример", SenderINN: "7700000000", SenderKPP: "770001001"}, nil
}

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "pek-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "pek", Family: sdk.FamilyLogistics, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

func TestRatesShipmentTrackingAndPickup(t *testing.T) {
	connector := NewWithConfiguration(candidateTransport{}, testConfigurationSource{}, nil)
	address := sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Примерная ул., 1"}
	parcel := sdk.Parcel{WeightGrams: 1000, LengthMM: 100, WidthMM: 100, HeightMM: 100}
	rates, err := connector.ReadLogisticsRates(context.Background(), testAccount(), testRuntime{}, sdk.RateRequest{From: address, To: address, Parcels: []sdk.Parcel{parcel}})
	if err != nil || len(rates) != 1 {
		t.Fatalf("rates=%+v err=%v", rates, err)
	}
	shipment, err := connector.CreateLogisticsShipment(context.Background(), testAccount(), testRuntime{}, sdk.ShipmentCreateRequest{ExternalID: "order:pek:1", ServiceCode: "pek_type_3", IdempotencyKey: "idem:pek:1", From: address, To: address, Parcels: []sdk.Parcel{parcel}, Sender: sdk.LogisticsContact{Name: "Отправитель", Phone: "+79990000000"}, Recipient: sdk.LogisticsContact{Name: "Получатель", Phone: "+79990000001"}})
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

func TestShipmentCancellationUsesCandidateTransport(t *testing.T) {
	shipment, err := New(candidateTransport{}, nil).CancelLogisticsShipment(context.Background(), testAccount(), testRuntime{}, sdk.ShipmentCancelRequest{RemoteID: "780339690775", IdempotencyKey: "cancel-pek-1"})
	if err != nil {
		t.Fatal(err)
	}
	if shipment.RemoteID != "780339690775" || shipment.Status != "cancelled" {
		t.Fatalf("unexpected cancellation result: %+v", shipment)
	}
}

func TestShipmentLabelUsesCandidateTransport(t *testing.T) {
	label, err := New(candidateTransport{}, nil).ReadLogisticsLabel(context.Background(), testAccount(), testRuntime{}, sdk.LabelRequest{RemoteID: "780339690775", Format: "pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if label.MediaType != "application/pdf" || label.ArtifactRef == "" || label.ObservedAt.IsZero() {
		t.Fatalf("unexpected label result: %+v", label)
	}
}

func TestHealthRejectsMissingRuntime(t *testing.T) {
	if _, err := New(candidateTransport{}, nil).Health(context.Background(), testAccount(), nil); err == nil {
		t.Fatal("expected missing runtime to be rejected")
	}
}
