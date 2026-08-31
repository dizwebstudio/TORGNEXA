package dellin

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type testRuntime struct{}

func (testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{} }

type testSecrets struct{}

func (testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, fn func([]byte) error) error {
	return fn([]byte(`{"appkey":"synthetic-app","pat":"synthetic-pat"}`))
}

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "dellin-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "dellin", Family: sdk.FamilyLogistics, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

func TestHealthUsesCandidateTransport(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	health, err := New(candidateTransport{}, func() time.Time { return now }).Health(context.Background(), testAccount(), testRuntime{})
	if err != nil || health.Status != sdk.HealthHealthy || !health.CheckedAt.Equal(now) {
		t.Fatalf("health=%+v err=%v", health, err)
	}
}

func TestPickupPointsUseCandidateTransport(t *testing.T) {
	points, err := New(candidateTransport{}, nil).ReadPickupPoints(context.Background(), testAccount(), testRuntime{}, sdk.PickupPointQuery{Country: "RU", City: "Санкт-Петербург", Limit: 10})
	if err != nil || len(points) != 1 || points[0].RemoteID != "39" {
		t.Fatalf("points=%+v err=%v", points, err)
	}
}

func TestRatesUseCandidateTransport(t *testing.T) {
	rates, err := New(candidateTransport{}, nil).ReadLogisticsRates(context.Background(), testAccount(), testRuntime{}, sdk.RateRequest{
		From:    sdk.Address{Country: "RU", City: "Москва", Line1: "Тверская, 1"},
		To:      sdk.Address{Country: "RU", City: "Санкт-Петербург", Line1: "Невский, 1"},
		Parcels: []sdk.Parcel{{WeightGrams: 1000, LengthMM: 100, WidthMM: 100, HeightMM: 100}},
	})
	if err != nil || len(rates) != 1 || rates[0].ServiceCode != "dellin_auto" {
		t.Fatalf("rates=%+v err=%v", rates, err)
	}
}

func TestTrackingUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).ReadLogisticsTracking(context.Background(), testAccount(), testRuntime{}, sdk.ShipmentStatusRequest{RemoteID: "400267443"})
	if err != nil || result.RemoteID != "400267443" || result.Status != "in_transit" || result.TrackingNumber != "400267443" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestLabelUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).ReadLogisticsLabel(context.Background(), testAccount(), testRuntime{}, sdk.LabelRequest{RemoteID: "0xad339ac31247666145816f2aeb4935ab", Format: "pdf"})
	if err != nil || result.MediaType != "application/pdf" || result.ArtifactRef == "" || result.ObservedAt.IsZero() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

type testConfigurationSource struct{ configuration Configuration }

func (source testConfigurationSource) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return source.configuration, nil
}

func testShipmentRequest() sdk.ShipmentCreateRequest {
	return sdk.ShipmentCreateRequest{
		ExternalID: "order-17", ServiceCode: "dellin_auto", IdempotencyKey: "create-17",
		From:      sdk.Address{Country: "RU", City: "Москва", Line1: "Тверская, 1"},
		To:        sdk.Address{Country: "RU", City: "Санкт-Петербург", Line1: "Невский, 1"},
		Parcels:   []sdk.Parcel{{WeightGrams: 1000, LengthMM: 100, WidthMM: 100, HeightMM: 100}},
		Sender:    sdk.LogisticsContact{Name: "ООО Торгнекса", Phone: "+74951234567"},
		Recipient: sdk.LogisticsContact{Name: "Иван Петров", Phone: "+79991234567"},
	}
}

func TestShipmentCreationRequiresExplicitConfiguration(t *testing.T) {
	_, err := New(candidateTransport{}, nil).CreateLogisticsShipment(context.Background(), testAccount(), testRuntime{}, testShipmentRequest())
	if err != ErrConfigurationMissing {
		t.Fatalf("expected missing configuration, got %v", err)
	}
}

func TestShipmentCreationUsesConfiguredRuntime(t *testing.T) {
	connector := New(candidateTransport{}, nil, testConfigurationSource{configuration: Configuration{
		RequesterUID: "requester-1", SenderCounteragentID: 123, FreightUID: "freight-1",
		ProduceDate: "2026-09-15", DerivalWorktimeStart: "09:00", DerivalWorktimeEnd: "18:00", PaymentType: "cash",
	}})
	result, err := connector.CreateLogisticsShipment(context.Background(), testAccount(), testRuntime{}, testShipmentRequest())
	if err != nil || result.RemoteID != "3954004" || result.Status != "created" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestShipmentCancellationReturnsPendingUntilCarrierConfirms(t *testing.T) {
	result, err := New(candidateTransport{}, nil).CancelLogisticsShipment(context.Background(), testAccount(), testRuntime{}, sdk.ShipmentCancelRequest{RemoteID: "3954004", IdempotencyKey: "cancel-17"})
	if err != nil || result.RemoteID != "3954004" || result.Status != "cancellation_pending" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

type rejectingSecrets struct{}

func (rejectingSecrets) UseSecret(context.Context, sdk.SecretReference, func([]byte) error) error {
	return errors.New("secret provider unavailable")
}

func TestHealthPropagatesSecretProviderFailure(t *testing.T) {
	_, err := New(candidateTransport{}, nil).Health(context.Background(), testAccount(), rejectingRuntime{})
	if err == nil || err.Error() != "secret provider unavailable" {
		t.Fatalf("expected secret provider failure, got %v", err)
	}
}

type rejectingRuntime struct{}

func (rejectingRuntime) Secrets() sdk.SecretAccessor { return rejectingSecrets{} }
