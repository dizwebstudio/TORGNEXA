// Package slo defines TORGNEXA's provider-neutral service-level objectives and
// deterministic repository performance qualification. It intentionally keeps
// production telemetry collection outside this package: adapters convert
// OpenTelemetry/Prometheus/Kafka/ClickHouse observations into Sample values.
package slo

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type Path string

const (
	PathAPI       Path = "api"
	PathKafka     Path = "kafka"
	PathSync      Path = "sync"
	PathWebhooks  Path = "webhooks"
	PathReporting Path = "reporting"
)

func (p Path) Valid() bool {
	switch p {
	case PathAPI, PathKafka, PathSync, PathWebhooks, PathReporting:
		return true
	default:
		return false
	}
}

type Objective struct {
	Path              Path
	AvailabilityMin   float64
	P50Max            time.Duration
	P95Max            time.Duration
	P99Max            time.Duration
	ThroughputMin     float64
	LagMax            time.Duration
	SaturationMax     float64
	ErrorBudgetWindow time.Duration
}

func (o Objective) Validate() error {
	if !o.Path.Valid() || o.AvailabilityMin <= 0 || o.AvailabilityMin > 1 ||
		o.P50Max <= 0 || o.P95Max < o.P50Max || o.P99Max < o.P95Max ||
		o.ThroughputMin <= 0 || o.LagMax < 0 || o.SaturationMax <= 0 || o.SaturationMax > 1 ||
		o.ErrorBudgetWindow <= 0 {
		return errors.New("slo: invalid objective")
	}
	return nil
}

func (o Objective) ErrorBudgetFraction() float64 { return 1 - o.AvailabilityMin }

type Observation struct {
	Latency    time.Duration
	Lag        time.Duration
	Operations uint64
	Errors     uint64
	Elapsed    time.Duration
	Busy       uint64
	Capacity   uint64
}

func (o Observation) Validate() error {
	if o.Latency < 0 || o.Lag < 0 || o.Operations == 0 || o.Errors > o.Operations || o.Elapsed <= 0 || o.Capacity == 0 || o.Busy > o.Capacity {
		return errors.New("slo: invalid observation")
	}
	return nil
}

type Summary struct {
	Count        int           `json:"count"`
	Availability float64       `json:"availability"`
	P50          time.Duration `json:"p50"`
	P95          time.Duration `json:"p95"`
	P99          time.Duration `json:"p99"`
	Throughput   float64       `json:"throughput_ops_per_second"`
	Lag          time.Duration `json:"lag"`
	Saturation   float64       `json:"saturation"`
}

func Summarize(observations []Observation) (Summary, error) {
	if len(observations) == 0 {
		return Summary{}, errors.New("slo: observations required")
	}
	latencies := make([]time.Duration, 0, len(observations))
	var operations, failures uint64
	var elapsed time.Duration
	var maxLag time.Duration
	var busy, capacity uint64
	for _, observation := range observations {
		if err := observation.Validate(); err != nil {
			return Summary{}, err
		}
		latencies = append(latencies, observation.Latency)
		operations += observation.Operations
		failures += observation.Errors
		elapsed += observation.Elapsed
		if observation.Lag > maxLag {
			maxLag = observation.Lag
		}
		busy += observation.Busy
		capacity += observation.Capacity
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	availability := 1 - float64(failures)/float64(operations)
	return Summary{
		Count: len(observations), Availability: availability,
		P50: percentile(latencies, .50), P95: percentile(latencies, .95), P99: percentile(latencies, .99),
		Throughput: float64(operations) / elapsed.Seconds(), Lag: maxLag,
		Saturation: float64(busy) / float64(capacity),
	}, nil
}

func percentile(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := int(math.Ceil(q*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

type Violation struct {
	Metric string `json:"metric"`
	Actual string `json:"actual"`
	Limit  string `json:"limit"`
}

type Evaluation struct {
	Path       Path        `json:"path"`
	Passed     bool        `json:"passed"`
	Summary    Summary     `json:"summary"`
	Violations []Violation `json:"violations,omitempty"`
}

func Evaluate(objective Objective, summary Summary) (Evaluation, error) {
	if err := objective.Validate(); err != nil {
		return Evaluation{}, err
	}
	if summary.Count <= 0 || summary.Availability < 0 || summary.Availability > 1 || summary.P50 < 0 || summary.P95 < summary.P50 || summary.P99 < summary.P95 || summary.Throughput < 0 || summary.Lag < 0 || summary.Saturation < 0 || summary.Saturation > 1 {
		return Evaluation{}, errors.New("slo: invalid summary")
	}
	out := Evaluation{Path: objective.Path, Passed: true, Summary: summary}
	add := func(metric, actual, limit string) {
		out.Passed = false
		out.Violations = append(out.Violations, Violation{Metric: metric, Actual: actual, Limit: limit})
	}
	if summary.Availability < objective.AvailabilityMin {
		add("availability", fmt.Sprintf("%.6f", summary.Availability), fmt.Sprintf(">= %.6f", objective.AvailabilityMin))
	}
	if summary.P50 > objective.P50Max {
		add("p50", summary.P50.String(), "<= "+objective.P50Max.String())
	}
	if summary.P95 > objective.P95Max {
		add("p95", summary.P95.String(), "<= "+objective.P95Max.String())
	}
	if summary.P99 > objective.P99Max {
		add("p99", summary.P99.String(), "<= "+objective.P99Max.String())
	}
	if summary.Throughput < objective.ThroughputMin {
		add("throughput", fmt.Sprintf("%.3f ops/s", summary.Throughput), fmt.Sprintf(">= %.3f ops/s", objective.ThroughputMin))
	}
	if summary.Lag > objective.LagMax {
		add("lag", summary.Lag.String(), "<= "+objective.LagMax.String())
	}
	if summary.Saturation > objective.SaturationMax {
		add("saturation", fmt.Sprintf("%.4f", summary.Saturation), fmt.Sprintf("<= %.4f", objective.SaturationMax))
	}
	return out, nil
}

type FailureMode string

const (
	FailureNone           FailureMode = "none"
	FailureBurst          FailureMode = "burst"
	FailureThrottle       FailureMode = "throttle"
	FailurePartialOutage  FailureMode = "partial_outage"
	FailureSlowDependency FailureMode = "slow_dependency"
)

type Profile struct {
	Name        string
	Path        Path
	Concurrency int
	Operations  int
	Failure     FailureMode
	Seed        uint64
}

func (p Profile) Validate() error {
	if strings.TrimSpace(p.Name) == "" || !p.Path.Valid() || p.Concurrency < 1 || p.Concurrency > 4096 || p.Operations < 100 || p.Operations > 10_000_000 || p.Seed == 0 {
		return errors.New("slo: invalid profile")
	}
	switch p.Failure {
	case FailureNone, FailureBurst, FailureThrottle, FailurePartialOutage, FailureSlowDependency:
	default:
		return errors.New("slo: invalid failure mode")
	}
	return nil
}

// Simulate produces deterministic observations for repository regression tests.
// It does not claim physical host capacity. Deployment qualification replaces
// these observations with measurements from the target topology.
func Simulate(profile Profile) ([]Observation, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	objective, ok := DefaultObjectives()[profile.Path]
	if !ok {
		return nil, errors.New("slo: objective not found")
	}
	n := 200
	out := make([]Observation, 0, n)
	base := objective.P50Max / 2
	for i := 0; i < n; i++ {
		jitter := time.Duration((uint64(i*37)+profile.Seed)%31) * base / 100
		latency := base + jitter
		lag := objective.LagMax / 4
		errorsCount := uint64(0)
		busyPct := uint64(55)
		switch profile.Failure {
		case FailureBurst:
			if i%20 < 3 {
				latency = objective.P95Max * 3 / 4
				busyPct = 78
			}
		case FailureThrottle:
			if i%10 < 2 {
				latency = objective.P95Max * 4 / 5
				lag = objective.LagMax * 3 / 4
				busyPct = 76
			}
		case FailurePartialOutage:
			if i%100 == 0 {
				errorsCount = 1
			}
		case FailureSlowDependency:
			if i%25 == 0 {
				latency = objective.P95Max * 9 / 10
				lag = objective.LagMax * 4 / 5
			}
		}
		ops := uint64(profile.Operations / n)
		if ops == 0 {
			ops = 1
		}
		elapsed := time.Duration(float64(time.Second) * float64(ops) / (objective.ThroughputMin * 1.25))
		if elapsed <= 0 {
			elapsed = time.Microsecond
		}
		out = append(out, Observation{Latency: latency, Lag: lag, Operations: ops, Errors: errorsCount, Elapsed: elapsed, Busy: busyPct, Capacity: 100})
	}
	return out, nil
}

func DefaultObjectives() map[Path]Objective {
	day := 30 * 24 * time.Hour
	return map[Path]Objective{
		PathAPI:       {Path: PathAPI, AvailabilityMin: .999, P50Max: 100 * time.Millisecond, P95Max: 300 * time.Millisecond, P99Max: 750 * time.Millisecond, ThroughputMin: 250, LagMax: 0, SaturationMax: .80, ErrorBudgetWindow: day},
		PathKafka:     {Path: PathKafka, AvailabilityMin: .9995, P50Max: 50 * time.Millisecond, P95Max: 250 * time.Millisecond, P99Max: time.Second, ThroughputMin: 1000, LagMax: 30 * time.Second, SaturationMax: .80, ErrorBudgetWindow: day},
		PathSync:      {Path: PathSync, AvailabilityMin: .995, P50Max: 500 * time.Millisecond, P95Max: 2 * time.Second, P99Max: 5 * time.Second, ThroughputMin: 20, LagMax: 5 * time.Minute, SaturationMax: .80, ErrorBudgetWindow: day},
		PathWebhooks:  {Path: PathWebhooks, AvailabilityMin: .999, P50Max: 250 * time.Millisecond, P95Max: 2 * time.Second, P99Max: 10 * time.Second, ThroughputMin: 50, LagMax: time.Minute, SaturationMax: .80, ErrorBudgetWindow: day},
		PathReporting: {Path: PathReporting, AvailabilityMin: .999, P50Max: 300 * time.Millisecond, P95Max: 2 * time.Second, P99Max: 5 * time.Second, ThroughputMin: 50, LagMax: time.Minute, SaturationMax: .80, ErrorBudgetWindow: day},
	}
}

func DefaultProfiles() []Profile {
	return []Profile{
		{Name: "api_steady", Path: PathAPI, Concurrency: 64, Operations: 100000, Failure: FailureNone, Seed: 101},
		{Name: "api_burst", Path: PathAPI, Concurrency: 256, Operations: 100000, Failure: FailureBurst, Seed: 102},
		{Name: "kafka_throttle", Path: PathKafka, Concurrency: 32, Operations: 200000, Failure: FailureThrottle, Seed: 201},
		{Name: "sync_partial_outage", Path: PathSync, Concurrency: 16, Operations: 20000, Failure: FailurePartialOutage, Seed: 301},
		{Name: "webhook_partial_outage", Path: PathWebhooks, Concurrency: 64, Operations: 50000, Failure: FailurePartialOutage, Seed: 401},
		{Name: "reporting_slow_sink", Path: PathReporting, Concurrency: 32, Operations: 50000, Failure: FailureSlowDependency, Seed: 501},
	}
}
