package background

import (
	"testing"
	"time"
)

func TestScheduledRunIDAndRetryJitterAreDeterministic(t *testing.T) {
	first := scheduledRunID("job-1", "policy-1")
	if first != scheduledRunID("job-1", "policy-1") || first == scheduledRunID("job-1", "policy-2") || len(first) > 128 {
		t.Fatalf("invalid deterministic run identity %q", first)
	}
	for attempt := 1; attempt <= 5; attempt++ {
		delay := retryDelay("job-1", attempt)
		minimum := time.Second << (attempt - 1)
		if delay < minimum || delay >= minimum+time.Second || delay != retryDelay("job-1", attempt) {
			t.Fatalf("attempt %d delay=%s", attempt, delay)
		}
	}
}
