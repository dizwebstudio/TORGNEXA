package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/torgnexa/torgnexa/internal/platform/slo"
)

type durationThresholds struct {
	AvailabilityMin   float64 `json:"availability_min"`
	P50Max            string  `json:"p50_max"`
	P95Max            string  `json:"p95_max"`
	P99Max            string  `json:"p99_max"`
	ThroughputMin     float64 `json:"throughput_min_ops_per_second"`
	LagMax            string  `json:"lag_max"`
	SaturationMax     float64 `json:"saturation_max"`
	ErrorBudgetWindow string  `json:"error_budget_window"`
}

type metrics struct {
	Availability float64 `json:"availability"`
	P50          string  `json:"p50"`
	P95          string  `json:"p95"`
	P99          string  `json:"p99"`
	Throughput   float64 `json:"throughput_ops_per_second"`
	Lag          string  `json:"lag"`
	Saturation   float64 `json:"saturation"`
}

type result struct {
	Name        string             `json:"name"`
	Path        slo.Path           `json:"path"`
	Concurrency int                `json:"concurrency"`
	Operations  int                `json:"operations"`
	FailureMode slo.FailureMode    `json:"failure_mode"`
	Thresholds  durationThresholds `json:"thresholds"`
	Baseline    metrics            `json:"baseline"`
	Passed      bool               `json:"passed"`
}

type report struct {
	SchemaVersion    int      `json:"schema_version"`
	Harness          string   `json:"harness"`
	MeasurementClass string   `json:"measurement_class"`
	Results          []result `json:"results"`
}

func main() {
	objectives := slo.DefaultObjectives()
	profiles := slo.DefaultProfiles()
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	out := report{SchemaVersion: 1, Harness: "torgnexa-normalized-slo-v1", MeasurementClass: "deterministic_repository_baseline"}
	failed := false
	for _, p := range profiles {
		obs, err := slo.Simulate(p)
		if err != nil {
			fatal(err)
		}
		summary, err := slo.Summarize(obs)
		if err != nil {
			fatal(err)
		}
		objective := objectives[p.Path]
		evaluation, err := slo.Evaluate(objective, summary)
		if err != nil {
			fatal(err)
		}
		if !evaluation.Passed {
			failed = true
		}
		out.Results = append(out.Results, result{Name: p.Name, Path: p.Path, Concurrency: p.Concurrency, Operations: p.Operations, FailureMode: p.Failure,
			Thresholds: durationThresholds{AvailabilityMin: objective.AvailabilityMin, P50Max: objective.P50Max.String(), P95Max: objective.P95Max.String(), P99Max: objective.P99Max.String(), ThroughputMin: objective.ThroughputMin, LagMax: objective.LagMax.String(), SaturationMax: objective.SaturationMax, ErrorBudgetWindow: objective.ErrorBudgetWindow.String()},
			Baseline:   metrics{Availability: summary.Availability, P50: summary.P50.String(), P95: summary.P95.String(), P99: summary.P99.String(), Throughput: summary.Throughput, Lag: summary.Lag.String(), Saturation: summary.Saturation}, Passed: evaluation.Passed})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fatal(err)
	}
	if failed {
		os.Exit(2)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
