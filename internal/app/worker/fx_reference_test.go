package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/fx"
)

type fxReferenceResolverStub struct {
	calls     int
	fail      bool
	failAfter int
}

func (stub *fxReferenceResolverStub) Resolve(_ context.Context, request fx.LookupRequest) (fx.RateFact, fx.ResolutionEvidence, error) {
	stub.calls++
	if request.Pair.Quote.String() != "RUB" || request.RateType != fx.RateOfficial || request.AsOf.Time().Location() != time.UTC {
		return fx.RateFact{}, fx.ResolutionEvidence{}, errors.New("unexpected request")
	}
	if stub.fail || (stub.failAfter > 0 && stub.calls > stub.failAfter) {
		return fx.RateFact{}, fx.ResolutionEvidence{}, errors.New("source unavailable")
	}
	return fx.RateFact{}, fx.ResolutionEvidence{}, nil
}

func TestRefreshFXReferenceRatesRejectsPartialBatch(t *testing.T) {
	resolver := &fxReferenceResolverStub{failAfter: 3}
	at := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	updated, err := refreshFXReferenceRates(context.Background(), resolver, at)
	if err == nil || updated != 3 || resolver.calls != len(cbrReferenceCurrencies) {
		t.Fatalf("updated=%d calls=%d err=%v", updated, resolver.calls, err)
	}
}

func TestRefreshFXReferenceRatesCoversCanonicalCurrencySet(t *testing.T) {
	resolver := &fxReferenceResolverStub{}
	at := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	updated, err := refreshFXReferenceRates(context.Background(), resolver, at)
	if err != nil {
		t.Fatal(err)
	}
	if updated != len(cbrReferenceCurrencies) || resolver.calls != len(cbrReferenceCurrencies) {
		t.Fatalf("updated=%d calls=%d currencies=%d", updated, resolver.calls, len(cbrReferenceCurrencies))
	}
}

func TestRefreshFXReferenceRatesFailsClosedWhenSourceUnavailable(t *testing.T) {
	resolver := &fxReferenceResolverStub{fail: true}
	at := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	updated, err := refreshFXReferenceRates(context.Background(), resolver, at)
	if err == nil || updated != 0 || resolver.calls != len(cbrReferenceCurrencies) {
		t.Fatalf("updated=%d calls=%d err=%v", updated, resolver.calls, err)
	}
}
