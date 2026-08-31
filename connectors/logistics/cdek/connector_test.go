package cdek

import (
	"context"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"testing"
	"time"
)

type rt struct{}

func (rt) Secrets() sdk.SecretAccessor { return candidateSecrets{} }
func acc() sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "c", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "cdek", Family: sdk.FamilyLogistics, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func TestRatesShipmentTrackLabelPickupAndReturn(t *testing.T) {
	c := New(candidateTransport{}, nil)
	addr := sdk.Address{Country: "RU", PostalCode: "101000", City: "Moscow", Line1: "Street 1"}
	parcel := sdk.Parcel{WeightGrams: 1000, LengthMM: 100, WidthMM: 100, HeightMM: 100}
	rates, e := c.ReadLogisticsRates(context.Background(), acc(), rt{}, sdk.RateRequest{From: addr, To: addr, Parcels: []sdk.Parcel{parcel}})
	if e != nil || len(rates) != 1 {
		t.Fatal(e)
	}
	sh, e := c.CreateLogisticsShipment(context.Background(), acc(), rt{}, sdk.ShipmentCreateRequest{ExternalID: "ord:1", ServiceCode: "canonical_service", IdempotencyKey: "idem:1", From: addr, To: addr, Parcels: []sdk.Parcel{parcel}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.ReadLogisticsTracking(context.Background(), acc(), rt{}, sdk.ShipmentStatusRequest{RemoteID: sh.RemoteID}); e != nil {
		t.Fatal(e)
	}
	if _, e = c.ReadLogisticsLabel(context.Background(), acc(), rt{}, sdk.LabelRequest{RemoteID: sh.RemoteID, Format: "pdf"}); e != nil {
		t.Fatal(e)
	}
	if _, e = c.ReadPickupPoints(context.Background(), acc(), rt{}, sdk.PickupPointQuery{Country: "RU", City: "Moscow", Limit: 10}); e != nil {
		t.Fatal(e)
	}
	if hook, hookErr := c.VerifyLogisticsWebhook(context.Background(), acc(), rt{}, []byte(`{"uuid":"order:1"}`), []byte("verified-signature")); hookErr != nil || hook.RemoteID != sh.RemoteID {
		t.Fatalf("webhook=%+v err=%v", hook, hookErr)
	}
	if result, returnErr := c.CreateLogisticsReturn(context.Background(), acc(), rt{}, sdk.ReturnCreateRequest{OriginalRemoteID: sh.RemoteID, ExternalID: "ret:1", MailType: "refusal", IdempotencyKey: "idem:2"}); returnErr != nil || result.Status != "created" || result.RemoteID == "" {
		t.Fatalf("unexpected CDEK refusal result: result=%+v err=%v", result, returnErr)
	}
	if result, returnErr := c.CreateLogisticsReturn(context.Background(), acc(), rt{}, sdk.ReturnCreateRequest{OriginalRemoteID: sh.RemoteID, ExternalID: "ret:2", MailType: "client_return", TariffCode: 136, IdempotencyKey: "idem:3"}); returnErr != nil || result.Status != "created" {
		t.Fatalf("unexpected CDEK client return result: result=%+v err=%v", result, returnErr)
	}
	if _, returnErr := c.CreateLogisticsReturn(context.Background(), acc(), rt{}, sdk.ReturnCreateRequest{OriginalRemoteID: sh.RemoteID, ExternalID: "ret:3", MailType: "client_return", IdempotencyKey: "idem:4"}); returnErr == nil {
		t.Fatal("client return without a tariff code was accepted")
	}
}
