package fivepost

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type candidateTransport struct{}

var candidateTime = time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)

func (candidateTransport) Ping(context.Context, []byte) error { return nil }

func (candidateTransport) Rates(context.Context, []byte, sdk.RateRequest) ([]sdk.RateQuote, error) {
	return []sdk.RateQuote{{ServiceCode: "fivepost_c2c", Cost: sdk.LogisticsMoney{MinorUnits: 33120, Currency: "RUB"}, MinDeliveryAt: candidateTime.Add(24 * time.Hour), MaxDeliveryAt: candidateTime.Add(48 * time.Hour), ObservedAt: candidateTime}}, nil
}

func candidateShipment(status string) sdk.ShipmentResult {
	return sdk.ShipmentResult{
		RemoteID:       "5post-order:1",
		Status:         status,
		Cost:           sdk.LogisticsMoney{MinorUnits: 0, Currency: "RUB"},
		TrackingNumber: "5POST-TRACK-1",
		ObservedAt:     candidateTime,
	}
}

func (candidateTransport) Create(context.Context, []byte, sdk.ShipmentCreateRequest, Configuration) (sdk.ShipmentResult, error) {
	return candidateShipment("created"), nil
}

type candidateConfigurationSource struct{}

func (candidateConfigurationSource) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{SenderLocation: "synthetic-warehouse", UndeliverableOption: "RETURN", BarcodeEnrichment: "ENABLED"}, nil
}

func (candidateTransport) Track(context.Context, []byte, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	return candidateShipment("in_transit"), nil
}

func (candidateTransport) Cancel(context.Context, []byte, sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	return candidateShipment("cancelled"), nil
}

func (candidateTransport) Label(context.Context, []byte, sdk.LabelRequest) (sdk.LabelResult, error) {
	return sdk.LabelResult{ArtifactRef: "artifact:5post-label:1", MediaType: "application/pdf", ObservedAt: candidateTime}, nil
}

func (candidateTransport) Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	return []sdk.PickupPoint{{RemoteID: "5post-pvz:1", Name: "5Post ПВЗ", Country: "RU", City: "Moscow", Address: "Example 5", Active: true, UpdatedAt: candidateTime}}, nil
}
