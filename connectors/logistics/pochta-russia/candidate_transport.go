package pochtarussia

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// candidateTransport is deterministic and never opens a network connection.
// It is used by unit and conformance tests only.
type candidateTransport struct{}

func (candidateTransport) Ping(context.Context, []byte) error { return nil }

func (candidateTransport) Rates(context.Context, []byte, sdk.RateRequest) ([]sdk.RateQuote, error) {
	observed := time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC)
	return []sdk.RateQuote{{
		ServiceCode:   "pochta_parcel_online",
		Cost:          sdk.LogisticsMoney{MinorUnits: 65000, Currency: "RUB"},
		MinDeliveryAt: observed.Add(3 * 24 * time.Hour),
		MaxDeliveryAt: observed.Add(6 * 24 * time.Hour),
		ObservedAt:    observed,
	}}, nil
}

func (candidateTransport) Pickup(_ context.Context, _ []byte, query sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	return []sdk.PickupPoint{{
		RemoteID: "101000", Name: "Почта России · ОПС 101000", Country: query.Country,
		City: query.City, Address: "Москва, Чистопрудный бульвар, 1", Active: true,
		UpdatedAt: time.Date(2026, 8, 28, 3, 0, 0, 0, time.UTC),
	}}, nil
}
