package reporting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

// HistoricalFXConverter is the narrow reporting dependency for explicit
// cross-currency derivation. The returned reference must identify the persisted
// immutable conversion record that contains the sourced rate facts and policy.
type HistoricalFXConverter interface {
	Convert(context.Context, string, domain.Money, domain.Currency, time.Time) (domain.Money, string, error)
}

type ConvertedSalesBucket struct {
	Day                  time.Time `json:"day"`
	SourceCurrency       string    `json:"source_currency"`
	TargetCurrency       string    `json:"target_currency"`
	Orders               uint64    `json:"orders"`
	FulfilledOrders      uint64    `json:"fulfilled_orders"`
	CancelledOrders      uint64    `json:"cancelled_orders"`
	GrossMinorUnits      int64     `json:"gross_minor_units"`
	FXConversionRecordID string    `json:"fx_conversion_record_id,omitempty"`
}

// ConvertSalesBucket converts exactly one already-aggregated original-currency
// bucket at a caller-selected UTC as-of instant. It never chooses a reporting
// date/rate implicitly; reproducible multi-currency totals are built only from
// these evidence-bearing converted buckets.
func ConvertSalesBucket(ctx context.Context, bucket SalesBucket, target domain.Currency, asOf time.Time, converter HistoricalFXConverter) (ConvertedSalesBucket, error) {
	if ctx == nil || bucket.Validate() != nil || target.Validate() != nil || asOf.IsZero() || !asOf.Equal(asOf.UTC()) {
		return ConvertedSalesBucket{}, ErrInvalid
	}
	sourceCurrency, err := domain.NewCurrency(bucket.Currency)
	if err != nil {
		return ConvertedSalesBucket{}, ErrInvalid
	}
	out := ConvertedSalesBucket{Day: bucket.Day, SourceCurrency: bucket.Currency, TargetCurrency: target.String(), Orders: bucket.Orders, FulfilledOrders: bucket.FulfilledOrders, CancelledOrders: bucket.CancelledOrders, GrossMinorUnits: bucket.GrossMinorUnits}
	if sourceCurrency == target {
		return out, nil
	}
	if converter == nil {
		return ConvertedSalesBucket{}, errors.New("reporting: historical FX converter required")
	}
	source, err := domain.NewMoney(bucket.GrossMinorUnits, sourceCurrency)
	if err != nil {
		return ConvertedSalesBucket{}, ErrInvalid
	}
	id := salesConversionID(bucket, target, asOf)
	converted, ref, err := converter.Convert(ctx, id, source, target, asOf)
	if err != nil {
		return ConvertedSalesBucket{}, err
	}
	if converted.Validate() != nil || converted.Currency() != target || ref == "" {
		return ConvertedSalesBucket{}, errors.New("reporting: invalid historical FX conversion result")
	}
	out.GrossMinorUnits = converted.MinorUnits()
	out.FXConversionRecordID = ref
	return out, nil
}

func salesConversionID(bucket SalesBucket, target domain.Currency, asOf time.Time) string {
	raw := bucket.Day.UTC().Format(time.RFC3339Nano) + "\x00" + bucket.Currency + "\x00" + target.String() + "\x00" + asOf.UTC().Format(time.RFC3339Nano)
	sum := sha256.Sum256([]byte(raw))
	return "reportfx:" + hex.EncodeToString(sum[:16])
}
