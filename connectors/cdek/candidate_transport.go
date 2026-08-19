package cdek

import (
	"context"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"time"
)

type candidateTransport struct{}

var ct = time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)

func (candidateTransport) Ping(context.Context, []byte) error { return nil }
func (candidateTransport) Rates(context.Context, []byte, sdk.RateRequest) ([]sdk.RateQuote, error) {
	return []sdk.RateQuote{{ServiceCode: "remote_tariff_1", Cost: sdk.LogisticsMoney{MinorUnits: 50000, Currency: "RUB"}, MinDeliveryAt: ct.Add(24 * time.Hour), MaxDeliveryAt: ct.Add(48 * time.Hour), ObservedAt: ct}}, nil
}
func shipment(status string) sdk.ShipmentResult {
	return sdk.ShipmentResult{RemoteID: "order:1", Status: status, Cost: sdk.LogisticsMoney{MinorUnits: 50000, Currency: "RUB"}, TrackingNumber: "TRACK1", ObservedAt: ct}
}
func (candidateTransport) Create(context.Context, []byte, sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	return shipment("created"), nil
}
func (candidateTransport) Track(context.Context, []byte, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	return shipment("in_transit"), nil
}
func (candidateTransport) Cancel(context.Context, []byte, sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	return shipment("cancelled"), nil
}
func (candidateTransport) Return(context.Context, []byte, sdk.ReturnCreateRequest) (sdk.ShipmentResult, error) {
	return shipment("return_created"), nil
}
func (candidateTransport) Label(context.Context, []byte, sdk.LabelRequest) (sdk.LabelResult, error) {
	return sdk.LabelResult{ArtifactRef: "artifact:label:1", MediaType: "application/pdf", ObservedAt: ct}, nil
}
func (candidateTransport) Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	return []sdk.PickupPoint{{RemoteID: "pvz:1", Name: "Pickup", Country: "RU", City: "Moscow", Address: "Example 1", Active: true, UpdatedAt: ct}}, nil
}

func (candidateTransport) Webhook(context.Context, []byte, []byte, []byte) (sdk.LogisticsWebhook, error) {
	return sdk.LogisticsWebhook{DeliveryID: "delivery:1", RemoteID: "order:1", Status: "in_transit", OccurredAt: ct}, nil
}
