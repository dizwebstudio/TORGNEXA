package dellin

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// candidateTransport is deterministic and never performs network I/O.
type candidateTransport struct{}

func (candidateTransport) Ping(context.Context, []byte) error { return nil }

func (candidateTransport) Rates(context.Context, []byte, sdk.RateRequest) ([]sdk.RateQuote, error) {
	now := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	return []sdk.RateQuote{{ServiceCode: "dellin_auto", Cost: sdk.LogisticsMoney{MinorUnits: 149900, Currency: "RUB"}, MinDeliveryAt: now.Add(48 * time.Hour), MaxDeliveryAt: now.Add(72 * time.Hour), ObservedAt: now}}, nil
}

func (candidateTransport) Track(_ context.Context, _ []byte, request sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{
		RemoteID: request.RemoteID, Status: "in_transit", TrackingNumber: request.RemoteID,
		Cost: sdk.LogisticsMoney{Currency: "RUB"}, ObservedAt: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC),
	}, nil
}

func (candidateTransport) Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	return []sdk.PickupPoint{{RemoteID: "39", Name: "Деловые Линии · Санкт-Петербург офис", Country: "RU", City: "Санкт-Петербург", Address: "Внуковская ул., 2а", Active: true, UpdatedAt: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)}}, nil
}
