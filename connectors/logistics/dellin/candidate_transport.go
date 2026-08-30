package dellin

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// candidateTransport is deterministic and never performs network I/O.
type candidateTransport struct{}

func (candidateTransport) Ping(context.Context, []byte) error { return nil }

func (candidateTransport) Track(_ context.Context, _ []byte, request sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{
		RemoteID: request.RemoteID, Status: "in_transit", TrackingNumber: request.RemoteID,
		Cost: sdk.LogisticsMoney{Currency: "RUB"}, ObservedAt: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC),
	}, nil
}

func (candidateTransport) Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	return []sdk.PickupPoint{{RemoteID: "39", Name: "Деловые Линии · Санкт-Петербург офис", Country: "RU", City: "Санкт-Петербург", Address: "Внуковская ул., 2а", Active: true, UpdatedAt: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)}}, nil
}
