package secretrepo

import (
	"testing"
	"time"
)

func TestOAuthRefreshBackoffIsExponentiallyBoundedAndJittered(t *testing.T) {
	t.Parallel()
	ceilings := []time.Duration{
		25 * time.Millisecond,
		50 * time.Millisecond,
		100 * time.Millisecond,
		200 * time.Millisecond,
		250 * time.Millisecond,
		250 * time.Millisecond,
	}
	for retry, ceiling := range ceilings {
		for range 128 {
			got := jitteredOAuthRefreshBackoff(uint(retry))
			if got < ceiling/2 || got > ceiling {
				t.Fatalf("retry %d backoff = %s, want [%s,%s]", retry, got, ceiling/2, ceiling)
			}
		}
	}
}

func TestDurationMetricsUseCumulativeFixedBuckets(t *testing.T) {
	t.Parallel()
	metrics := newAtomicDurationMetrics([]time.Duration{10 * time.Millisecond, 50 * time.Millisecond})
	metrics.record(5 * time.Millisecond)
	metrics.record(25 * time.Millisecond)
	metrics.record(100 * time.Millisecond)
	snapshot := metrics.snapshot()
	if snapshot.Count != 3 || snapshot.Total != 130*time.Millisecond || len(snapshot.Buckets) != 2 || snapshot.Buckets[0].Count != 1 || snapshot.Buckets[1].Count != 2 {
		t.Fatalf("unexpected duration metrics: %+v", snapshot)
	}
}
