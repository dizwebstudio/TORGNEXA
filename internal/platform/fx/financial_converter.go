package fx

import (
	"context"
	"errors"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

// MinorUnitRegistry is an explicit currency metadata snapshot used by financial
// consumers. It is immutable after construction so a historical conversion
// cannot silently change scale because of process-local configuration drift.
type MinorUnitRegistry struct{ scales map[domain.Currency]uint8 }

func NewMinorUnitRegistry(values map[domain.Currency]uint8) (MinorUnitRegistry, error) {
	if len(values) == 0 {
		return MinorUnitRegistry{}, errors.New("FX minor-unit registry is empty")
	}
	copyValues := make(map[domain.Currency]uint8, len(values))
	for currency, scale := range values {
		if currency.Validate() != nil || scale > domain.MaxDecimalScale {
			return MinorUnitRegistry{}, errors.New("invalid FX minor-unit registry")
		}
		copyValues[currency] = scale
	}
	return MinorUnitRegistry{scales: copyValues}, nil
}

func (r MinorUnitRegistry) Scale(currency domain.Currency) (uint8, bool) {
	if currency.Validate() != nil {
		return 0, false
	}
	scale, ok := r.scales[currency]
	return scale, ok
}

// FinancialConverter is the narrow bridge consumed by reporting and payment
// reconciliation. Every cross-currency result is backed by a persisted
// ConversionRecord; callers receive that immutable record ID as lineage.
type FinancialConverter struct {
	resolver      *Resolver
	scales        MinorUnitRegistry
	rateType      RateType
	rounding      RoundingPolicy
	triangulation TriangulationPolicy
}

func NewFinancialConverter(resolver *Resolver, scales MinorUnitRegistry, rateType RateType, rounding RoundingPolicy, triangulation TriangulationPolicy) (*FinancialConverter, error) {
	if resolver == nil || rateType.Validate() != nil || rounding.Validate() != nil || triangulation.Validate() != nil || len(scales.scales) == 0 {
		return nil, errors.New("invalid financial FX converter")
	}
	return &FinancialConverter{resolver: resolver, scales: scales, rateType: rateType, rounding: rounding, triangulation: triangulation}, nil
}

// Convert implements the provider-neutral financial consumer port used by
// reporting/payment reconciliation. conversionID must be stable for the
// business derivation so retries append the same immutable record.
func (c *FinancialConverter) Convert(ctx context.Context, conversionID string, source domain.Money, target domain.Currency, asOf time.Time) (domain.Money, string, error) {
	if c == nil || c.resolver == nil || ctx == nil || conversionID == "" || source.Validate() != nil || target.Validate() != nil || asOf.IsZero() || !asOf.Equal(asOf.UTC()) {
		return domain.Money{}, "", errors.New("invalid financial FX conversion")
	}
	if source.Currency() == target {
		return source, "", nil
	}
	sourceScale, ok := c.scales.Scale(source.Currency())
	if !ok {
		return domain.Money{}, "", errors.New("missing source currency minor-unit scale")
	}
	targetScale, ok := c.scales.Scale(target)
	if !ok {
		return domain.Money{}, "", errors.New("missing target currency minor-unit scale")
	}
	instant, err := domain.NewUTCInstant(asOf)
	if err != nil {
		return domain.Money{}, "", err
	}
	record, err := c.resolver.Convert(ctx, ConversionRequest{
		ID:                   conversionID,
		Source:               source,
		SourceMinorUnitScale: sourceScale,
		TargetCurrency:       target,
		TargetMinorUnitScale: targetScale,
		AsOf:                 instant,
		RateType:             c.rateType,
		Rounding:             c.rounding,
		Triangulation:        c.triangulation,
	})
	if err != nil {
		return domain.Money{}, "", err
	}
	return record.Snapshot.TargetAmount, record.ID, nil
}
