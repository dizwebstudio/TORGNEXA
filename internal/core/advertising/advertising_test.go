package advertising

import (
	"errors"
	"testing"
	"time"
)

var advertisingTestTime = time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

func spend(id string, amount int64) SpendFact {
	return SpendFact{ID: id, AccountID: "account", Channel: "marketplace", CampaignID: "campaign", RemoteFactID: "remote-" + id, PeriodStart: advertisingTestTime.Add(-24 * time.Hour), PeriodEnd: advertisingTestTime, AmountMinor: amount, Currency: "RUB", Source: "api", ObservedAt: advertisingTestTime, EffectiveAt: advertisingTestTime, Quality: QualityConfirmed}
}

func performance(id string, orders, revenue int64) PerformanceFact {
	return PerformanceFact{ID: id, AccountID: "account", Channel: "marketplace", CampaignID: "campaign", RemoteFactID: "remote-" + id, PeriodStart: advertisingTestTime.Add(-24 * time.Hour), PeriodEnd: advertisingTestTime, Impressions: 100, Clicks: 10, Orders: orders, RevenueMinor: revenue, Currency: "RUB", Source: "api", ObservedAt: advertisingTestTime, EffectiveAt: advertisingTestTime, Quality: QualityConfirmed}
}

func TestAggregateUsesIntegerRates(t *testing.T) {
	metrics, err := Aggregate([]SpendFact{spend("1", 2500)}, []PerformanceFact{performance("1", 2, 10000)})
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 1 || metrics[0].ROASBasisPoints != 40000 || metrics[0].ROMIBasisPoints != 30000 || metrics[0].DRRBasisPoints != 2500 || metrics[0].OrderCostMinor != 1250 {
		t.Fatalf("metrics=%+v", metrics)
	}
}

func TestDeduplicateSpendRejectsConflictingRemoteFact(t *testing.T) {
	first := spend("1", 2500)
	second := first
	second.ID = "2"
	second.AmountMinor = 2600
	if _, err := DeduplicateSpend([]SpendFact{first, second}); !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
}

func TestDeduplicatePerformanceRejectsConflictingRemoteFact(t *testing.T) {
	first := performance("1", 2, 2500)
	second := first
	second.ID = "2"
	second.Orders = 3
	if _, err := DeduplicatePerformance([]PerformanceFact{first, second}); !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateRejectsNonUTCFact(t *testing.T) {
	fact := spend("1", 1)
	fact.PeriodStart = fact.PeriodStart.In(time.FixedZone("MSK", 3*60*60))
	if !errors.Is(fact.Validate(), ErrInvalid) {
		t.Fatal("non-UTC fact accepted")
	}
}
