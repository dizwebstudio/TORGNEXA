package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/marketplaceoperations"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

func TestMarketplaceFindingActionExecutorFuncFailsClosedWhenUnset(t *testing.T) {
	var executor MarketplaceFindingActionExecutorFunc
	if !errors.Is(executor.Execute(context.Background(), tenancy.Scope{}, marketplaceoperations.Finding{}, marketplaceoperations.FindingAction{}), ErrMarketplaceActionExecutorUnavailable) {
		t.Fatal("unset marketplace action executor must fail closed")
	}
}

func TestMarketplaceActionErrorCodeNeverLeaksArbitraryErrorText(t *testing.T) {
	if got := marketplaceActionErrorCode(errors.New("provider timeout")); got != "marketplace_action_failed" {
		t.Fatalf("unexpected unsafe error code: %q", got)
	}
	if got := marketplaceActionErrorCode(errors.New("provider_timeout")); got != "provider_timeout" {
		t.Fatalf("safe error code was changed: %q", got)
	}
	if got := marketplaceActionErrorCode(errors.New("")); got != "marketplace_action_failed" {
		t.Fatalf("empty error code was accepted: %q", got)
	}
}
