package marketplaceoperations

import "testing"

func TestEvaluateKeepsMarketplaceAccountPartiallySupportedUntilEvidence(t *testing.T) {
	matrix := Evaluate(AccountInput{
		AccountID: "account-1", ConnectorID: "marketplace-a", AccountStatus: "active", HealthStatus: "healthy",
		Capabilities: []Capability{
			{Name: "products.read", Enabled: true}, {Name: "products.write", Status: "enabled"},
			{Name: "inventory.read", Enabled: true}, {Name: "ads.read", Enabled: true},
		},
	})
	byKey := make(map[string]Operation, len(matrix.Operations))
	for _, operation := range matrix.Operations {
		byKey[operation.Key] = operation
	}
	if got := byKey["catalog"].State; got != StatePartiallySupported {
		t.Fatalf("catalog state = %q, want %q", got, StatePartiallySupported)
	}
	if got := byKey["inventory"].State; got != StateReadOnly {
		t.Fatalf("inventory state = %q, want %q", got, StateReadOnly)
	}
	if got := byKey["advertising"].State; got != StateReadOnly {
		t.Fatalf("advertising state = %q, want %q", got, StateReadOnly)
	}
	if got := byKey["orders"].State; got != StateNotAvailable {
		t.Fatalf("orders state = %q, want %q", got, StateNotAvailable)
	}
	if got := byKey["marking_upd"].ReasonCode; got != "requires_separate_government_and_edo_accounts" {
		t.Fatalf("marking reason = %q", got)
	}
}

func TestEvaluateBlocksWritesForUnhealthyAccount(t *testing.T) {
	matrix := Evaluate(AccountInput{AccountID: "account-1", AccountStatus: "active", HealthStatus: "unavailable", Capabilities: []Capability{{Name: "products.write", Enabled: true}}})
	for _, operation := range matrix.Operations {
		if operation.State != StateBlocked {
			t.Fatalf("operation %q state = %q, want %q", operation.Key, operation.State, StateBlocked)
		}
	}
}

func TestEvaluateUsesQualificationEvidenceOnlyForCompleteOperation(t *testing.T) {
	matrix := Evaluate(AccountInput{
		AccountID: "account-1", AccountStatus: "active", HealthStatus: "healthy",
		Capabilities: []Capability{{Name: "products.read", Enabled: true}, {Name: "products.write", Enabled: true}},
		Qualified:    map[string]bool{"catalog": true},
	})
	for _, operation := range matrix.Operations {
		if operation.Key == "catalog" && operation.State != StateQualified {
			t.Fatalf("catalog state = %q, want %q", operation.State, StateQualified)
		}
	}
	if matrix.OverallState != StatePartiallySupported {
		t.Fatalf("overall state = %q, want partial because other operations are not qualified", matrix.OverallState)
	}

	all := AccountInput{
		AccountID: "account-1", AccountStatus: "active", HealthStatus: "healthy",
		Capabilities: []Capability{
			{Name: "products.read", Enabled: true}, {Name: "products.write", Enabled: true},
			{Name: "prices.read", Enabled: true}, {Name: "prices.write", Enabled: true},
			{Name: "inventory.read", Enabled: true}, {Name: "inventory.write", Enabled: true},
			{Name: "orders.read", Enabled: true}, {Name: "orders.status.write", Enabled: true},
			{Name: "returns.read", Enabled: true}, {Name: "ads.read", Enabled: true}, {Name: "ads.manage", Enabled: true},
			{Name: "finance.settlements.read", Enabled: true},
			{Name: "marking.codes.request", Enabled: true}, {Name: "marking.aggregation.write", Enabled: true}, {Name: "edo.documents.send", Enabled: true},
		},
		Qualified: map[string]bool{"catalog": true, "pricing": true, "inventory": true, "orders": true, "fulfillment": true, "returns": true, "advertising": true, "settlement_pnl": true, "marking_upd": true},
	}
	allMatrix := Evaluate(all)
	if allMatrix.OverallState != StateQualified {
		t.Fatalf("fully qualified matrix overall state = %q, want %q", allMatrix.OverallState, StateQualified)
	}
}

func TestEvaluateWriteOnlyCapabilityIsNotReadOnly(t *testing.T) {
	matrix := Evaluate(AccountInput{
		AccountID: "account-1", ConnectorID: "marketplace-a", AccountStatus: "active", HealthStatus: "healthy",
		Capabilities: []Capability{{Name: "prices.write", Enabled: true}},
	})
	for _, operation := range matrix.Operations {
		if operation.Key != "pricing" {
			continue
		}
		if operation.State != StatePartiallySupported || operation.ReasonCode != "capability_set_incomplete" {
			t.Fatalf("pricing state=%s reason=%s, want partially_supported/capability_set_incomplete", operation.State, operation.ReasonCode)
		}
		return
	}
	t.Fatal("pricing operation missing")
}
