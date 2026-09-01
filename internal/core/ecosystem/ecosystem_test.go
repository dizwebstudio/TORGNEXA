package ecosystem

import (
	"testing"
	"time"
)

func testEvidence(at time.Time, kind string) Evidence {
	return Evidence{Kind: kind, SourceRef: "evidence/task-231", Digest: digestFor("test-evidence"), CheckedAt: at, Environment: "sandbox"}
}

func testPortfolio(at time.Time) PortfolioItem {
	return PortfolioItem{ID: "connector.catalog", Kind: KindConnector, Tier: "commerce", DisplayName: "Catalog connector", Status: StatusIntegrated, Owner: "commerce-platform", Priority: "wave_1", Decision: "deepen", NextAction: "sandbox_conformance", SupportLevel: "repository", Deployment: "community", UseCases: []string{"catalog", "orders"}, Capabilities: []string{"products.read"}, Version: 1, UpdatedAt: at}
}

func TestPromoteRequiresExactQualificationEvidence(t *testing.T) {
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	item := testPortfolio(at)
	item, err := Promote(item, StatusVerified, testEvidence(at, "repository"), at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Promote(item, StatusReady, testEvidence(at, "repository"), at); err != nil {
		t.Fatal(err)
	}
	item.Status = StatusReady
	if _, err := Promote(item, StatusQualified, testEvidence(at, "repository"), at); err != ErrPromotion {
		t.Fatalf("repository evidence must not qualify, got %v", err)
	}
	if _, err := Promote(item, StatusQualified, testEvidence(at, "credentialed_sandbox"), at); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluateOnboardingIsFailClosed(t *testing.T) {
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	run := OnboardingRun{ID: "onboard-1", ResourceID: "connector.catalog", State: OnboardingDraft, OwnerRef: "operator-1", IdempotencyKey: "onboard-key-1", Version: 1, CreatedAt: at, UpdatedAt: at, Checks: []OnboardingCheck{{ID: "auth", Label: "Scoped access", Required: true, State: CheckPassed, UpdatedAt: at}, {ID: "live", Label: "Credentialed qualification", Required: true, State: CheckPending, UpdatedAt: at}}}
	result, err := EvaluateOnboarding(run)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != OnboardingRunning {
		t.Fatalf("got %s, want running", result.State)
	}
	result.Checks[1].State = CheckFailed
	result, err = EvaluateOnboarding(result)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != OnboardingBlocked {
		t.Fatalf("got %s, want blocked", result.State)
	}
}

func TestBuildOverviewDoesNotTurnCountsIntoSupport(t *testing.T) {
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	mobile, tiers, support, err := DefaultSurfaces(at)
	if err != nil {
		t.Fatal(err)
	}
	overview, err := BuildOverview(OverviewInput{Now: at, Portfolio: []PortfolioItem{testPortfolio(at)}, VisibleApps: 3, Mobile: mobile, HostedTiers: tiers, Support: support, ExternalGates: []string{"hosted_topology", "partner_certification"}})
	if err != nil {
		t.Fatal(err)
	}
	if overview.StatusCounts.Integrated != 1 || overview.VisibleApps != 3 {
		t.Fatalf("unexpected overview: %+v", overview.StatusCounts)
	}
	if overview.HostedTiers[1].SLAClaimable {
		t.Fatal("hosted tier cannot claim SLA without topology evidence")
	}
}
