package pek

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// candidateTransport is deterministic and never opens a network connection.
// It is used by unit and conformance tests only.
type candidateTransport struct{}

var candidateTime = time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC)

func (candidateTransport) Ping(context.Context, []byte) error { return nil }

func (candidateTransport) Rates(context.Context, []byte, sdk.RateRequest) ([]sdk.RateQuote, error) {
	return []sdk.RateQuote{{ServiceCode: "pek-road", Cost: sdk.LogisticsMoney{MinorUnits: 50000, Currency: "RUB"}, MinDeliveryAt: candidateTime.Add(48 * time.Hour), MaxDeliveryAt: candidateTime.Add(96 * time.Hour), ObservedAt: candidateTime}}, nil
}

func candidateShipment(status string) sdk.ShipmentResult {
	return sdk.ShipmentResult{RemoteID: "pek-cargo:1", Status: status, Cost: sdk.LogisticsMoney{MinorUnits: 50000, Currency: "RUB"}, TrackingNumber: "PEK-TRACK-1", ObservedAt: candidateTime}
}

func (candidateTransport) Create(context.Context, []byte, sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	return candidateShipment("created"), nil
}

func (candidateTransport) Track(context.Context, []byte, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	return candidateShipment("in_transit"), nil
}

func (candidateTransport) Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	return []sdk.PickupPoint{{RemoteID: "pek-branch:1", Name: "ПЭК · Центральный терминал", Country: "RU", City: "Москва", Address: "Примерная ул., 1", Active: true, UpdatedAt: candidateTime}}, nil
}
