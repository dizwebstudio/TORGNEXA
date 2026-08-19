package slo

import (
	"testing"
	"time"
)

func TestDefaultProfilesMeetRepositoryBaseline(t *testing.T) {
	objectives := DefaultObjectives()
	for _, profile := range DefaultProfiles() {
		t.Run(profile.Name, func(t *testing.T) {
			observations, err := Simulate(profile)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := Summarize(observations)
			if err != nil {
				t.Fatal(err)
			}
			result, err := Evaluate(objectives[profile.Path], summary)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Passed {
				t.Fatalf("baseline failed: %+v", result.Violations)
			}
			if !(summary.P50 <= summary.P95 && summary.P95 <= summary.P99) {
				t.Fatalf("percentiles out of order: %+v", summary)
			}
		})
	}
}

func TestEvaluatorFailsClosedOnEveryThreshold(t *testing.T) {
	objective := DefaultObjectives()[PathAPI]
	base := Summary{Count: 100, Availability: .9999, P50: 50 * time.Millisecond, P95: 100 * time.Millisecond, P99: 200 * time.Millisecond, Throughput: 500, Lag: 0, Saturation: .5}
	cases := []struct {
		name   string
		mutate func(*Summary)
	}{
		{"availability", func(s *Summary) { s.Availability = .9 }},
		{"p50", func(s *Summary) { s.P50 = 101 * time.Millisecond; s.P95 = s.P50; s.P99 = s.P50 }},
		{"p95", func(s *Summary) { s.P95 = 301 * time.Millisecond; s.P99 = s.P95 }},
		{"p99", func(s *Summary) { s.P99 = 751 * time.Millisecond }},
		{"throughput", func(s *Summary) { s.Throughput = 249 }},
		{"saturation", func(s *Summary) { s.Saturation = .81 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			tc.mutate(&s)
			r, err := Evaluate(objective, s)
			if err != nil {
				t.Fatal(err)
			}
			if r.Passed {
				t.Fatal("expected violation")
			}
		})
	}
}

func TestLagThreshold(t *testing.T) {
	objective := DefaultObjectives()[PathKafka]
	summary := Summary{Count: 10, Availability: 1, P50: time.Millisecond, P95: time.Millisecond, P99: time.Millisecond, Throughput: 2000, Lag: 31 * time.Second, Saturation: .2}
	result, err := Evaluate(objective, summary)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed || len(result.Violations) != 1 || result.Violations[0].Metric != "lag" {
		t.Fatalf("unexpected result %+v", result)
	}
}

func TestSummarizeWeightsOperationsAndUsesMaxLag(t *testing.T) {
	s, err := Summarize([]Observation{
		{Latency: 10 * time.Millisecond, Lag: time.Second, Operations: 100, Errors: 0, Elapsed: time.Second, Busy: 5, Capacity: 10},
		{Latency: 20 * time.Millisecond, Lag: 3 * time.Second, Operations: 100, Errors: 1, Elapsed: time.Second, Busy: 7, Capacity: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Availability != .995 || s.Throughput != 100 || s.Lag != 3*time.Second || s.Saturation != .6 {
		t.Fatalf("unexpected summary %+v", s)
	}
}

func TestInvalidInputFailsClosed(t *testing.T) {
	if _, err := Simulate(Profile{}); err == nil {
		t.Fatal("invalid profile accepted")
	}
	if _, err := Summarize(nil); err == nil {
		t.Fatal("empty observations accepted")
	}
	if _, err := Evaluate(Objective{}, Summary{}); err == nil {
		t.Fatal("invalid objective accepted")
	}
}
