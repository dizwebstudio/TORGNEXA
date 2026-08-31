package worker

import (
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/operatorassistant"
)

func TestAssistantRunTransitionIsMonotonic(t *testing.T) {
	if err := assistantRunTransition(operatorassistant.RunQueued, operatorassistant.RunRetrievingContext); err != nil {
		t.Fatal(err)
	}
	if err := assistantRunTransition(operatorassistant.RunCompleted, operatorassistant.RunQueued); err == nil {
		t.Fatal("terminal run moved backwards")
	}
	if err := assistantRunTransition(operatorassistant.RunQueued, operatorassistant.RunProviderUnavailable); err != nil {
		t.Fatalf("queued run cannot be recovered without provider: %v", err)
	}
	if assistantRetryable("provider_timeout") != true || assistantRetryable("policy_denied") {
		t.Fatal("retry policy is not bounded")
	}
}

func TestAssistantLeaseDeadlineIsUTCAndBounded(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.FixedZone("MSK", 3*60*60))
	deadline := assistantLeaseDeadline(now)
	if deadline.Location() != time.UTC || deadline.Sub(now.UTC()) != 45*time.Second {
		t.Fatalf("deadline = %s", deadline)
	}
}
