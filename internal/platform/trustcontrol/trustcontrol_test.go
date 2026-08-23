package trustcontrol

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestCalculateScenarioUsesFixedDecimal(t *testing.T) {
	result, err := CalculateScenario(ScenarioInput{Name: "base", SaleCurrency: "rub", CostCurrency: "usd", QuantityMilli: 2500, SaleUnitPriceMinor: 10000, CostUnitMinor: 3000, LogisticsTotalCostMinor: 1000, AdvertisingTotalCostMinor: 500, MarketplaceFeeBasisPoints: 1200, CostToSaleFXRateMicros: 2_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if result.RevenueMinor != 25000 || result.MarketplaceFeeMinor != 3000 || result.ConvertedCostsMinor != 18000 || result.ContributionProfitMinor != 4000 || result.MarginBasisPoints != 1600 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestAIEgressRedactsAndDeniesUnknownClass(t *testing.T) {
	policy := Policy{Version: 1, Enabled: true, AllowedDataClasses: []string{"aggregate"}, AllowedDestinations: []string{"destination"}, AllowedModels: []string{"model"}, MaxPromptBytes: 1000, MonthlyRequestLimit: 2}
	if err := AuthorizeEgress(policy, EgressRequest{Destination: "destination", Model: "model", DataClasses: []string{"personal"}, PromptBytes: 10}); !errors.Is(err, ErrDenied) {
		t.Fatalf("expected deny, got %v", err)
	}
	redacted, err := RedactPrompt("email user@example.com token=abcd1234", 1000)
	if err != nil || redacted == "email user@example.com token=abcd1234" {
		t.Fatalf("redaction failed: %q %v", redacted, err)
	}
}

func TestReplayRequiresSyntheticAndRejectsSecrets(t *testing.T) {
	if err := ValidateReplayTarget("marketplace", "catalog.products.read"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateReplayTarget("../../etc", "read"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid replay target accepted: %v", err)
	}
	if _, _, err := ValidateSyntheticFixture(json.RawMessage(`{"_synthetic":true,"items":[{"sku":"S-1"}]}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ValidateSyntheticFixture(json.RawMessage(`{"_synthetic":true,"api_token":"secret"}`)); !errors.Is(err, ErrDenied) {
		t.Fatalf("credential-shaped fixture accepted: %v", err)
	}
}
