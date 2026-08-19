package reporting

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

type reportingFX struct {
	money domain.Money
	ref   string
	err   error
}

func (f reportingFX) Convert(_ context.Context, _ string, _ domain.Money, _ domain.Currency, _ time.Time) (domain.Money, string, error) {
	return f.money, f.ref, f.err
}

func TestConvertSalesBucketRequiresEvidenceForCrossCurrency(t *testing.T) {
	day := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	rub, _ := domain.NewCurrency("RUB")
	money, _ := domain.NewMoney(805000, rub)
	bucket := SalesBucket{Day: day, Currency: "USD", Orders: 2, FulfilledOrders: 2, GrossMinorUnits: 10000}
	out, err := ConvertSalesBucket(context.Background(), bucket, rub, day.Add(12*time.Hour), reportingFX{money: money, ref: "reportfx:record:1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.GrossMinorUnits != 805000 || out.FXConversionRecordID != "reportfx:record:1" || out.SourceCurrency != "USD" || out.TargetCurrency != "RUB" {
		t.Fatalf("out=%+v", out)
	}
}

func TestConvertSalesBucketFailsWhenFXUnavailable(t *testing.T) {
	day := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	rub, _ := domain.NewCurrency("RUB")
	bucket := SalesBucket{Day: day, Currency: "USD", Orders: 1, GrossMinorUnits: 100}
	_, err := ConvertSalesBucket(context.Background(), bucket, rub, day.Add(time.Hour), reportingFX{err: errors.New("stale")})
	if err == nil {
		t.Fatal("expected explicit FX failure")
	}
}
