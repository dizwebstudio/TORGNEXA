package uniteconomics

import (
	"errors"
	"testing"
	"time"
)

func financialTestInput() FinancialInput {
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	cogs := int64(3000)
	return FinancialInput{OrganizationID: "org", WorkspaceID: "ws", Basis: BasisOrderAccrual, From: now, To: now.Add(24 * time.Hour), SaleLines: []SaleLineFact{{ID: "line-1", OrderID: "order-1", SKU: "SKU-1", ChannelRef: "channel:market", Currency: "RUB", OccurredAt: now.Add(time.Hour), QuantityMilli: 1000, GrossMinor: 10000, DiscountMinor: 1000, COGSMinor: &cogs}}, Facts: []FinancialFact{{ID: "fee", SourceSystem: "market", SourceAccount: "account", SourceRef: "fee-1", IdempotencyKey: "fee-1", OrderID: "order-1", SKU: "SKU-1", Kind: FactCommission, AmountMinor: -1000, Currency: "RUB", OccurredAt: now.Add(2 * time.Hour)}, {ID: "logistics", SourceSystem: "market", SourceAccount: "account", SourceRef: "log-1", IdempotencyKey: "log-1", OrderID: "order-1", SKU: "SKU-1", Kind: FactLogistics, AmountMinor: -500, Currency: "RUB", OccurredAt: now.Add(2 * time.Hour)}, {ID: "payout", SourceSystem: "market", SourceAccount: "account", SourceRef: "pay-1", IdempotencyKey: "pay-1", OrderID: "order-1", SKU: "SKU-1", Kind: FactPayout, AmountMinor: 8500, Currency: "RUB", OccurredAt: now.Add(3 * time.Hour)}}}
}

func TestCalculateFinancialUsesFormulaAndPayoutIsNotRevenue(t *testing.T) {
	snapshot, err := CalculateFinancial(financialTestInput(), time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Rows) != 1 {
		t.Fatalf("rows=%d", len(snapshot.Rows))
	}
	row := snapshot.Rows[0]
	if row.NetSalesMinor != 9000 {
		t.Fatalf("net sales=%d", row.NetSalesMinor)
	}
	if row.ContributionMinor != 4500 {
		t.Fatalf("contribution=%d", row.ContributionMinor)
	}
	if row.GrossMinor == row.NetSalesMinor {
		t.Fatal("payout leaked into revenue")
	}
	if row.QualityStatus == QualityComplete {
		t.Fatal("missing storage/advertising should be visible")
	}
}

func TestCalculateFinancialDeduplicatesAndRejectsFactCollision(t *testing.T) {
	in := financialTestInput()
	in.Facts = append(in.Facts, in.Facts[0])
	if _, err := CalculateFinancial(in, time.Now().UTC()); err != nil {
		t.Fatalf("same fact should be idempotent: %v", err)
	}
	in.Facts[len(in.Facts)-1].AmountMinor = -1001
	if _, err := CalculateFinancial(in, time.Now().UTC()); !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestCalculateFinancialMissingCOGSIsNotZero(t *testing.T) {
	in := financialTestInput()
	in.SaleLines[0].COGSMinor = nil
	snapshot, err := CalculateFinancial(in, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Rows[0].COGSMinor != 0 || snapshot.Rows[0].COGSStatus != ValueMissing || snapshot.Rows[0].QualityStatus != QualityMissingCOGS {
		t.Fatalf("missing cogs was hidden: %+v", snapshot.Rows[0])
	}
}

func TestValueFIFOHandlesLotsMovesAndPartialSales(t *testing.T) {
	at := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	result, err := ValueFIFO([]StockMovement{{ID: "r1", SKU: "SKU-1", WarehouseID: "w1", Kind: FIFOMovementReceive, QuantityMilli: 2000, UnitCostMinor: 1000, Currency: "RUB", SourceRef: "r1", OccurredAt: at}, {ID: "r2", SKU: "SKU-1", WarehouseID: "w1", Kind: FIFOMovementReceive, QuantityMilli: 1000, UnitCostMinor: 1500, Currency: "RUB", SourceRef: "r2", OccurredAt: at.Add(time.Hour)}, {ID: "m1", SKU: "SKU-1", FromWarehouseID: "w1", ToWarehouseID: "w2", Kind: FIFOMovementMoveOut, QuantityMilli: 1000, UnitCostMinor: 0, Currency: "RUB", SourceRef: "m1", OccurredAt: at.Add(2 * time.Hour)}, {ID: "m2", SKU: "SKU-1", ToWarehouseID: "w2", RelatedMovementID: "m1", Kind: FIFOMovementMoveIn, QuantityMilli: 1000, UnitCostMinor: 0, Currency: "RUB", SourceRef: "m2", OccurredAt: at.Add(3 * time.Hour)}, {ID: "s1", SKU: "SKU-1", WarehouseID: "w2", Kind: FIFOMovementSale, QuantityMilli: 500, UnitCostMinor: 0, Currency: "RUB", SourceRef: "s1", OccurredAt: at.Add(4 * time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Allocations) != 1 || result.Allocations[0].CostMinor != 500 {
		t.Fatalf("fifo=%+v", result.Allocations)
	}
}

func TestValueFIFOReportsUnavailableCost(t *testing.T) {
	at := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	_, err := ValueFIFO([]StockMovement{{ID: "s1", SKU: "SKU-1", WarehouseID: "w1", Kind: FIFOMovementSale, QuantityMilli: 1000, UnitCostMinor: 0, Currency: "RUB", SourceRef: "s1", OccurredAt: at}})
	if !errors.Is(err, ErrFIFOCostUnavailable) {
		t.Fatalf("want unavailable, got %v", err)
	}
}
