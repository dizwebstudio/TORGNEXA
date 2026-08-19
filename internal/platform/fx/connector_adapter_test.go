package fx

import (
	"context"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

type fxReaderStub struct{ observation sdk.FXRateObservation }

func (s fxReaderStub) ReadFXRate(context.Context, sdk.Account, sdk.Runtime, sdk.FXRateRequest) (sdk.FXRateObservation, error) {
	return s.observation, nil
}

func TestConnectorProviderConvertsSDKObservationToImmutableFact(t *testing.T) {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	account := sdk.Account{ID: "fx-test-account", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "fx-test", Family: sdk.FamilyFX, Status: sdk.AccountActive, Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
	source, _ := NewSourceID("reference")
	provider, err := NewConnectorProvider(source, fxReaderStub{sdk.FXRateObservation{ID: "reference:2026-08-12:USD:RUB:x", BaseCurrency: "USD", QuoteCurrency: "RUB", Rate: "80.5", Source: "reference", SourceReference: "daily/2026-08-12/rate1", RateType: "official", ObservedAt: at, EffectiveAt: at}}, account, nil)
	if err != nil {
		t.Fatal(err)
	}
	usd, _ := domain.NewCurrency("USD")
	rub, _ := domain.NewCurrency("RUB")
	pair, _ := NewPair(usd, rub)
	instant, _ := domain.NewUTCInstant(at)
	fact, err := provider.Lookup(context.Background(), LookupRequest{Pair: pair, AsOf: instant, RateType: RateOfficial})
	if err != nil {
		t.Fatal(err)
	}
	if fact.Source() != source || fact.Pair() != pair || fact.Rate().String() != "80.5" {
		t.Fatalf("fact=%+v", fact)
	}
}
