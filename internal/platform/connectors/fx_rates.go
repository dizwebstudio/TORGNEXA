package connectors

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

var currencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)

type FXRateRequest struct {
	BaseCurrency  string
	QuoteCurrency string
	AsOf          time.Time
	RateType      string
}

func (r FXRateRequest) Validate() error {
	if !currencyCodePattern.MatchString(r.BaseCurrency) || !currencyCodePattern.MatchString(r.QuoteCurrency) || r.BaseCurrency == r.QuoteCurrency || r.AsOf.IsZero() || r.AsOf.Location() != time.UTC || !safeCodePattern.MatchString(r.RateType) {
		return errors.New("connectors: invalid FX rate request")
	}
	return nil
}

type FXRateObservation struct {
	ID              string
	BaseCurrency    string
	QuoteCurrency   string
	Rate            string
	Source          string
	SourceReference string
	RateType        string
	ObservedAt      time.Time
	EffectiveAt     time.Time
}

func (o FXRateObservation) Validate() error {
	if o.ID == "" || len(o.ID) > 128 || !currencyCodePattern.MatchString(o.BaseCurrency) || !currencyCodePattern.MatchString(o.QuoteCurrency) || o.BaseCurrency == o.QuoteCurrency || o.Rate == "" || len(o.Rate) > 64 || !safeCodePattern.MatchString(o.Source) || !safeCodePattern.MatchString(o.RateType) || o.ObservedAt.IsZero() || o.EffectiveAt.IsZero() || o.ObservedAt.Location() != time.UTC || o.EffectiveAt.Location() != time.UTC || len(o.SourceReference) > 256 || strings.ContainsAny(o.SourceReference, "?&#=\r\n\t") {
		return errors.New("connectors: invalid FX rate observation")
	}
	return nil
}

// FXRateReader is the additive SDK-v1 operation surface for fx.rates.read.
// It contains only provider-neutral strings/timestamps; exact money/rate domain
// validation remains host-owned.
type FXRateReader interface {
	ReadFXRate(context.Context, Account, Runtime, FXRateRequest) (FXRateObservation, error)
}
