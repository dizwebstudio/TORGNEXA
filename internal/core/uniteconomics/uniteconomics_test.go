package uniteconomics

import (
	"math"
	"testing"
	"time"
)

func testOrder(id, channel string, status string, gross, discount, grand int64) OrderFact {
	return OrderFact{ID: id, ChannelRef: channel, Currency: "RUB", OccurredAt: time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), QuantityMilli: 1000, GrossMinor: gross, DiscountMinor: discount, GrandMinor: grand, Status: status}
}

func TestCalculateIsDeterministicAndDoesNotCountPayoutAsRevenue(t *testing.T) {
	in := Input{OrganizationID: "org-1", WorkspaceID: "ws-1", Basis: BasisOrderAccrual, From: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), Orders: []OrderFact{testOrder("o-1", "channel:store", "fulfilled", 10000, 500, 9500)}, Settlements: []SettlementFact{{ID: "s-fee", SourceSystem: "market", SourceAccount: "a", EntryRef: "fee-1", OrderID: "o-1", ChannelRef: "channel:store", Kind: "fee", AmountMinor: -1000, Currency: "RUB", OccurredAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)}, {ID: "s-payout", SourceSystem: "market", SourceAccount: "a", EntryRef: "payout-1", OrderID: "o-1", ChannelRef: "channel:store", Kind: "payout", AmountMinor: 8500, Currency: "RUB", OccurredAt: time.Date(2026, 8, 1, 13, 0, 0, 0, time.UTC)}}}
	first, err := Calculate(in, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Calculate(in, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if first.InputDigest != second.InputDigest || first.Rows[0].Contribution.MinorUnits != second.Rows[0].Contribution.MinorUnits {
		t.Fatal("same input must produce the same result")
	}
	row := first.Rows[0]
	if row.NetRevenue.MinorUnits != 9500 || row.Payout.MinorUnits != 8500 || row.Contribution.MinorUnits != 8500 {
		t.Fatalf("unexpected economics: %+v", row)
	}
	if row.QualityStatus != QualityPartial {
		t.Fatalf("missing COGS/logistics must be visible: %s", row.QualityStatus)
	}
}

func TestCalculateDeduplicatesSettlementAndDetectsCollision(t *testing.T) {
	base := Input{OrganizationID: "org-1", WorkspaceID: "ws-1", Basis: BasisSettlement, From: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), Orders: []OrderFact{testOrder("o-1", "channel:market", "fulfilled", 10000, 0, 10000)}, Settlements: []SettlementFact{{ID: "s-1", SourceSystem: "market", SourceAccount: "a", EntryRef: "same", OrderID: "o-1", ChannelRef: "channel:market", Kind: "fee", AmountMinor: -100, Currency: "RUB", OccurredAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)}, {ID: "s-1", SourceSystem: "market", SourceAccount: "a", EntryRef: "same", OrderID: "o-1", ChannelRef: "channel:market", Kind: "fee", AmountMinor: -100, Currency: "RUB", OccurredAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)}}}
	snapshot, err := Calculate(base, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Rows[0].Commission.MinorUnits != 100 {
		t.Fatalf("duplicate fee counted twice: %+v", snapshot.Rows[0])
	}
	base.Settlements[1].AmountMinor = -101
	if _, err := Calculate(base, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)); err != ErrConflict {
		t.Fatalf("collision error=%v", err)
	}
}

func TestCalculateRejectsMixedCurrencyAndOverflow(t *testing.T) {
	in := Input{OrganizationID: "org-1", WorkspaceID: "ws-1", Basis: BasisOrderAccrual, ReportingCurrency: "RUB", From: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), Orders: []OrderFact{testOrder("o-1", "channel:one", "fulfilled", 10, 0, 10), {ID: "o-2", ChannelRef: "channel:two", Currency: "USD", OccurredAt: time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), QuantityMilli: 1000, GrossMinor: 10, GrandMinor: 10, Status: "fulfilled"}}}
	if _, err := Calculate(in, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)); err != ErrMixedCurrency {
		t.Fatalf("mixed currency error=%v", err)
	}
	if _, err := Calculate(Input{OrganizationID: "o", WorkspaceID: "w", Basis: BasisOrderAccrual, From: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), Orders: []OrderFact{{ID: "x", Currency: "RUB", OccurredAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), QuantityMilli: 1, GrossMinor: math.MaxInt64, GrandMinor: math.MaxInt64, Status: "fulfilled"}, {ID: "y", Currency: "RUB", OccurredAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), QuantityMilli: 1, GrossMinor: 1, GrandMinor: 1, Status: "fulfilled"}}}, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("overflow must fail closed")
	}
}
