package worker

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/fx"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/fxrepo"
)

const fxReferenceRefreshInterval = 6 * time.Hour

var cbrReferenceCurrencies = []string{
	"AUD", "AZN", "DZD", "GBP", "AMD", "BHD", "BYN", "BOB", "BRL", "HUF",
	"VND", "HKD", "GEL", "DKK", "AED", "USD", "EUR", "EGP", "INR", "IDR",
	"KZT", "CAD", "QAR", "KGS", "CNY", "CUP", "MDL", "MNT", "NGN",
	"NZD", "NOK", "OMR", "PLN", "SAR", "RON", "XDR", "SGD", "TJS", "THB",
	"BDT", "TRY", "TMT", "UZS", "UAH", "CZK", "SEK", "CHF", "ETB", "RSD",
	"ZAR", "KRW", "JPY", "MMK",
}

type fxReferenceResolver interface {
	Resolve(context.Context, fx.LookupRequest) (fx.RateFact, fx.ResolutionEvidence, error)
}

func newFXReferenceResolver(database *sql.DB, registry *builtinruntime.Registry) (*fx.Resolver, error) {
	if database == nil || registry == nil {
		return nil, errors.New("worker: FX reference dependencies required")
	}
	repository, err := fxrepo.New(database)
	if err != nil {
		return nil, err
	}
	sources, err := registry.FXReferenceSources()
	if err != nil || len(sources) == 0 {
		return nil, errors.New("worker: FX reference provider unavailable")
	}
	source := sources[0].ID()
	precedence, err := fx.NewSourcePrecedence(source)
	if err != nil {
		return nil, err
	}
	return fx.NewResolver(repository, sources, precedence, map[fx.SourceID]fx.FreshnessPolicy{
		source: {MaxEffectiveAge: 14 * 24 * time.Hour},
	}, fx.NewMemoryCache(30*time.Minute, 512), func() time.Time { return time.Now().UTC() })
}

func refreshFXReferenceRates(ctx context.Context, resolver fxReferenceResolver, at time.Time) (int, error) {
	if ctx == nil || resolver == nil || at.IsZero() || at.Location() != time.UTC {
		return 0, errors.New("worker: invalid FX reference refresh")
	}
	quote, err := domain.NewCurrency("RUB")
	if err != nil {
		return 0, err
	}
	asOf, err := domain.NewUTCInstant(at)
	if err != nil {
		return 0, err
	}
	updated := 0
	var lastErr error
	for _, code := range cbrReferenceCurrencies {
		base, currencyErr := domain.NewCurrency(code)
		if currencyErr != nil {
			return updated, currencyErr
		}
		pair, pairErr := fx.NewPair(base, quote)
		if pairErr != nil {
			return updated, pairErr
		}
		if _, _, resolveErr := resolver.Resolve(ctx, fx.LookupRequest{Pair: pair, AsOf: asOf, RateType: fx.RateOfficial}); resolveErr != nil {
			lastErr = resolveErr
			continue
		}
		updated++
	}
	if updated != len(cbrReferenceCurrencies) {
		if lastErr == nil {
			lastErr = errors.New("worker: FX reference source returned an incomplete rate set")
		}
		return updated, lastErr
	}
	return updated, nil
}

func runFXReferenceRefresh(ctx context.Context, logger *slog.Logger, resolver fxReferenceResolver) error {
	if logger == nil || resolver == nil {
		return errors.New("worker: FX reference component dependencies required")
	}
	return pollLoop(ctx, fxReferenceRefreshInterval, func() error {
		updated, err := refreshFXReferenceRates(ctx, resolver, time.Now().UTC())
		if err != nil {
			logger.Warn("FX reference refresh deferred", "event", "worker.fx_reference_deferred", "error_code", "reference_source_incomplete", "rates", updated, "expected", len(cbrReferenceCurrencies))
			return nil
		}
		logger.Info("FX reference rates refreshed", "event", "worker.fx_reference_refreshed", "source", "cbr", "rates", updated)
		return nil
	})
}
