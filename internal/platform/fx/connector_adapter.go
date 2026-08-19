package fx

import (
	"context"
	"errors"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

type ConnectorProvider struct {
	source  SourceID
	reader  sdk.FXRateReader
	account sdk.Account
	runtime sdk.Runtime
}

func NewConnectorProvider(source SourceID, reader sdk.FXRateReader, account sdk.Account, runtime sdk.Runtime) (*ConnectorProvider, error) {
	if source.Validate() != nil || reader == nil || account.Validate() != nil {
		return nil, errors.New("invalid FX connector provider")
	}
	return &ConnectorProvider{source: source, reader: reader, account: account, runtime: runtime}, nil
}
func (p *ConnectorProvider) ID() SourceID { return p.source }
func (p *ConnectorProvider) Lookup(ctx context.Context, req LookupRequest) (RateFact, error) {
	if p == nil || ctx == nil || req.Validate() != nil {
		return RateFact{}, ErrRateMissing
	}
	obs, err := p.reader.ReadFXRate(ctx, p.account, p.runtime, sdk.FXRateRequest{BaseCurrency: req.Pair.Base.String(), QuoteCurrency: req.Pair.Quote.String(), AsOf: req.AsOf.Time(), RateType: string(req.RateType)})
	if err != nil {
		return RateFact{}, err
	}
	if obs.Validate() != nil || obs.Source != p.source.String() {
		return RateFact{}, ErrSourceResultMismatch
	}
	base, err := domain.NewCurrency(obs.BaseCurrency)
	if err != nil {
		return RateFact{}, ErrSourceResultMismatch
	}
	quote, err := domain.NewCurrency(obs.QuoteCurrency)
	if err != nil {
		return RateFact{}, ErrSourceResultMismatch
	}
	pair, err := NewPair(base, quote)
	if err != nil {
		return RateFact{}, ErrSourceResultMismatch
	}
	rate, err := domain.ParseDecimal(obs.Rate)
	if err != nil {
		return RateFact{}, ErrSourceResultMismatch
	}
	source, err := NewSourceID(obs.Source)
	if err != nil {
		return RateFact{}, ErrSourceResultMismatch
	}
	observed, err := domain.NewUTCInstant(obs.ObservedAt)
	if err != nil {
		return RateFact{}, ErrSourceResultMismatch
	}
	effective, err := domain.NewUTCInstant(obs.EffectiveAt)
	if err != nil {
		return RateFact{}, ErrSourceResultMismatch
	}
	fact, err := NewRateFact(RateFactInput{ID: obs.ID, Pair: pair, Rate: rate, Source: source, SourceReference: obs.SourceReference, RateType: RateType(obs.RateType), ObservedAt: observed, EffectiveAt: effective})
	if err != nil {
		return RateFact{}, ErrSourceResultMismatch
	}
	if ValidateLookupResult(p.source, req, fact) != nil {
		return RateFact{}, ErrSourceResultMismatch
	}
	return fact, nil
}
