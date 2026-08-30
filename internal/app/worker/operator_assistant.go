package worker

import (
	"errors"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/operatorassistant"
)

var errInvalidAssistantTransition = errors.New("worker: invalid operator assistant run transition")

// assistantRunTransition is the worker's monotonic lifecycle gate. The API
// creates queued runs; a future provider adapter may advance them, but no
// transition can move a run back to an earlier state or execute an action.
func assistantRunTransition(from, to operatorassistant.RunState) error {
	if from == to {
		return nil
	}
	allowed := map[operatorassistant.RunState][]operatorassistant.RunState{
		operatorassistant.RunQueued:            {operatorassistant.RunRetrievingContext, operatorassistant.RunCancelled, operatorassistant.RunFailed},
		operatorassistant.RunRetrievingContext: {operatorassistant.RunAwaitingModel, operatorassistant.RunPartial, operatorassistant.RunProviderUnavailable, operatorassistant.RunCancelled, operatorassistant.RunFailed},
		operatorassistant.RunAwaitingModel:     {operatorassistant.RunStreaming, operatorassistant.RunAwaitingApproval, operatorassistant.RunCompleted, operatorassistant.RunPartial, operatorassistant.RunStale, operatorassistant.RunProviderUnavailable, operatorassistant.RunCancelled, operatorassistant.RunFailed},
		operatorassistant.RunStreaming:         {operatorassistant.RunAwaitingApproval, operatorassistant.RunCompleted, operatorassistant.RunPartial, operatorassistant.RunStale, operatorassistant.RunCancelled, operatorassistant.RunFailed},
		operatorassistant.RunAwaitingApproval:  {operatorassistant.RunActionQueued, operatorassistant.RunCancelled, operatorassistant.RunFailed},
		operatorassistant.RunActionQueued:      {operatorassistant.RunCompleted, operatorassistant.RunPartial, operatorassistant.RunFailed},
	}
	for _, candidate := range allowed[from] {
		if candidate == to {
			return nil
		}
	}
	return errInvalidAssistantTransition
}

func assistantRetryable(errorCode string) bool {
	switch errorCode {
	case "source_timeout", "provider_timeout", "provider_rate_limited", "provider_unavailable":
		return true
	default:
		return false
	}
}

// assistantLeaseDeadline bounds how long a worker may hold a run lease.
func assistantLeaseDeadline(now time.Time) time.Time {
	return now.UTC().Add(45 * time.Second)
}
