package replenishment

import (
	"errors"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

func testQuantity(t *testing.T, value string, unit string) domain.Quantity {
	t.Helper()
	decimal, err := domain.ParseDecimal(value)
	if err != nil {
		t.Fatalf("parse decimal %q: %v", value, err)
	}
	code, err := domain.NewUnitCode(unit)
	if err != nil {
		t.Fatalf("parse unit %q: %v", unit, err)
	}
	quantity, err := domain.NewQuantity(decimal, code)
	if err != nil {
		t.Fatalf("new quantity: %v", err)
	}
	return quantity
}

func testObservation(t *testing.T, id, sku, quantity, returns string) DemandObservation {
	t.Helper()
	when := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, len(id))
	return DemandObservation{
		ID:          id,
		Grain:       PlanningGrain{OfferID: "offer-" + sku, SKU: sku, WarehouseID: "warehouse-1"},
		BucketStart: when,
		ObservedAt:  when.Add(time.Hour),
		Quantity:    testQuantity(t, quantity, "PCS"),
		Returns:     testQuantity(t, returns, "PCS"),
		Source:      "orders",
	}
}

func TestDigestObservationsIsOrderIndependentAndNetsReturns(t *testing.T) {
	first := testObservation(t, "obs-1", "SKU-1", "10", "2")
	second := testObservation(t, "obs-2", "SKU-1", "12", "0")
	digestA, err := DigestObservations([]DemandObservation{first, second})
	if err != nil {
		t.Fatal(err)
	}
	digestB, err := DigestObservations([]DemandObservation{second, first})
	if err != nil {
		t.Fatal(err)
	}
	if digestA != digestB {
		t.Fatalf("digest depends on input order: %s != %s", digestA, digestB)
	}
	net, err := first.NetDemand()
	if err != nil {
		t.Fatal(err)
	}
	if net.Value.String() != "8" {
		t.Fatalf("expected net demand 8, got %s", net.Value.String())
	}
}

func TestBuildForecastCarriesLatestAndObservedMaximum(t *testing.T) {
	observations := []DemandObservation{
		testObservation(t, "obs-1", "SKU-1", "10", "2"),
		testObservation(t, "obs-2", "SKU-1", "12", "0"),
	}
	generatedAt := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	run, err := NewForecastRun("run-1", "org-1", "workspace-1", "latest_observation_v1", 5, observations, generatedAt)
	if err != nil {
		t.Fatal(err)
	}
	points, err := BuildForecast(run, observations, ForecastConfig{PeriodDays: 2, MinimumSamples: 2, AlgorithmVersion: "latest_observation_v1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 3 {
		t.Fatalf("expected 3 forecast points, got %d", len(points))
	}
	if points[0].DemandP50.Value.String() != "12" || points[0].DemandP90.Value.String() != "12" || points[0].SampleCount != 2 {
		t.Fatalf("unexpected point: %+v", points[0])
	}
	if err := points[0].Validate(); err != nil {
		t.Fatalf("forecast point must validate: %v", err)
	}
	_, err = BuildForecast(run, observations, ForecastConfig{PeriodDays: 2, MinimumSamples: 2, AlgorithmVersion: "different_version"})
	if !errors.Is(err, ErrPlanningConflict) {
		t.Fatalf("expected algorithm version conflict, got %v", err)
	}
}

func TestBuildForecastFailsClosedOnColdStart(t *testing.T) {
	now := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	run, err := NewForecastRun("run-empty", "org-1", "workspace-1", "latest_observation_v1", 7, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = BuildForecast(run, nil, ForecastConfig{PeriodDays: 1, MinimumSamples: 1, AlgorithmVersion: "latest_observation_v1"})
	if !errors.Is(err, ErrInsufficientData) {
		t.Fatalf("expected insufficient data, got %v", err)
	}
}

func TestProjectStockKeepsShortfallVisible(t *testing.T) {
	projection, err := ProjectStock(
		"run-1",
		PlanningGrain{OfferID: "offer-1", SKU: "SKU-1", WarehouseID: "warehouse-1"},
		time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC),
		testQuantity(t, "3", "PCS"),
		testQuantity(t, "1", "PCS"),
		testQuantity(t, "7", "PCS"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if projection.ProjectedAvailable.Value.String() != "0" || projection.Shortfall.Value.String() != "3" || !projection.StockoutRisk {
		t.Fatalf("shortfall was not preserved: %+v", projection)
	}
	if err := projection.Validate(); err != nil {
		t.Fatalf("projection must validate: %v", err)
	}
}

func TestRoundToPackHonorsMOQAndCasePack(t *testing.T) {
	quantity, err := RoundToPack(testQuantity(t, "3", "PCS"), testQuantity(t, "5", "PCS"), testQuantity(t, "4", "PCS"))
	if err != nil {
		t.Fatal(err)
	}
	if quantity.Value.String() != "8" {
		t.Fatalf("expected 8 units after pack rounding, got %s", quantity.Value.String())
	}
	_, err = RoundToPack(testQuantity(t, "3", "PCS"), testQuantity(t, "1", "KG"), testQuantity(t, "1", "KG"))
	if !errors.Is(err, ErrUnitMismatch) {
		t.Fatalf("expected unit mismatch, got %v", err)
	}
}

func TestPurchasePlanAutoSubmitRequiresApprovalAndOpenKillSwitch(t *testing.T) {
	plan := PurchasePlan{
		ID:               "plan-1",
		RecommendationID: "recommendation-1",
		SupplierOfferID:  "supplier-offer-1",
		Mode:             AutoSubmit,
		Status:           "draft",
		Quantity:         testQuantity(t, "4", "PCS"),
		EstimatedCost:    func() domain.Money { value, _ := domain.NewMoney(100, "RUB"); return value }(),
		IdempotencyKey:   "plan-1-idempotency",
		ApprovalRequired: false,
		KillSwitchActive: false,
		CreatedAt:        time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC),
		Version:          1,
	}
	if err := plan.Validate(); !errors.Is(err, ErrModeNotAllowed) {
		t.Fatalf("expected approval guard, got %v", err)
	}
	plan.ApprovalRequired = true
	plan.KillSwitchActive = true
	if err := plan.Validate(); !errors.Is(err, ErrModeNotAllowed) {
		t.Fatalf("expected kill-switch guard, got %v", err)
	}
}
