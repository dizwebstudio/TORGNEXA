package fivepost

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
	return callback([]byte("synthetic-5post-key"))
}

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{
		ID: "fivepost-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002",
		ConnectorID: "fivepost", Family: sdk.FamilyLogistics, Status: sdk.AccountActive,
		SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1,
		Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at,
	}
}

func TestShipmentTrackingLabelPickupAndCancel(t *testing.T) {
	connector := NewWithConfiguration(candidateTransport{}, candidateConfigurationSource{}, nil)
	address := sdk.Address{Country: "RU", PostalCode: "101000", City: "Moscow", Line1: "Street 5"}
	parcel := sdk.Parcel{WeightGrams: 1000, LengthMM: 100, WidthMM: 100, HeightMM: 100}
	shipment, err := connector.CreateLogisticsShipment(context.Background(), testAccount(), testRuntime{}, sdk.ShipmentCreateRequest{
		ExternalID: "order:5post:1", ServiceCode: "pickup", IdempotencyKey: "idem:5post:1", From: address, To: address, Parcels: []sdk.Parcel{parcel}, PickupPointRef: "pvz:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = connector.ReadLogisticsTracking(context.Background(), testAccount(), testRuntime{}, sdk.ShipmentStatusRequest{RemoteID: shipment.RemoteID}); err != nil {
		t.Fatal(err)
	}
	if _, err = connector.ReadLogisticsLabel(context.Background(), testAccount(), testRuntime{}, sdk.LabelRequest{RemoteID: shipment.RemoteID, Format: "pdf"}); err != nil {
		t.Fatal(err)
	}
	if _, err = connector.ReadPickupPoints(context.Background(), testAccount(), testRuntime{}, sdk.PickupPointQuery{Country: "RU", City: "Moscow", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err = connector.CancelLogisticsShipment(context.Background(), testAccount(), testRuntime{}, sdk.ShipmentCancelRequest{RemoteID: shipment.RemoteID, IdempotencyKey: "idem:5post:cancel:1"}); err != nil {
		t.Fatal(err)
	}
}

func TestHealthRejectsMissingCredentials(t *testing.T) {
	connector := New(candidateTransport{}, nil)
	if _, err := connector.Health(context.Background(), testAccount(), nil); err == nil {
		t.Fatal("expected missing runtime to be rejected")
	}
}

func TestLogisticsRatesRequiresAndNormalizesPointReferences(t *testing.T) {
	connector := New(candidateTransport{}, nil)
	address := sdk.Address{Country: "RU", PostalCode: "101000", City: "Moscow", Line1: "Street 5"}
	quotes, err := connector.ReadLogisticsRates(context.Background(), testAccount(), testRuntime{}, sdk.RateRequest{
		From: address, To: address, FromPointRef: "13e9d62d-1799-4e14-a27b-d218f33de7f6", ToPointRef: "23e9d62d-1799-4e14-a27b-d218f33de7f6",
		Parcels: []sdk.Parcel{{WeightGrams: 1000, LengthMM: 100, WidthMM: 100, HeightMM: 100}},
	})
	if err != nil || len(quotes) != 1 || quotes[0].ServiceCode != "fivepost_c2c" {
		t.Fatalf("unexpected 5Post rates: quotes=%+v err=%v", quotes, err)
	}
}
