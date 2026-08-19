# Task 066 — SLO and performance test suite

## What is qualified

Task 066 converts the earlier observability checklist into an executable SLO
contract for API, Kafka/event processing, sync/reconciliation, durable webhooks
and ClickHouse reporting. `internal/platform/slo` owns objective validation,
nearest-rank percentile calculation, throughput/lag/saturation summaries,
error-budget math and fail-closed evaluation.

## Repository baseline

`performance/baseline-v1.json` is generated from deterministic profiles:

- `api_steady`: steady 64-way API load;
- `api_burst`: 256-way burst pressure;
- `kafka_throttle`: broker/consumer throttling and lag pressure;
- `sync_partial_outage`: bounded remote/provider failure;
- `webhook_partial_outage`: retryable endpoint failure;
- `reporting_slow_sink`: degraded analytical dependency.

The committed baseline records p50/p95/p99, throughput, lag and saturation next
to the threshold used to judge each result. `make performance` regenerates the
file and fails on drift, preventing silent relaxation of the workload or SLO.

This normalized harness is intentionally deterministic and hardware-neutral. It
qualifies the SLO machinery and failure profiles; it does **not** claim that a
particular laptop or shared CI runner can sustain production traffic.

## Deployment qualification

Before production release, the same objectives must be populated from real
telemetry on the exact topology. Evidence must state:

1. commit/image digest and database/broker versions;
2. CPU/RAM/storage/network and replica/partition counts;
3. dataset/cardinality and retained history;
4. concurrency, operation mix and test duration;
5. p50/p95/p99, throughput, lag, saturation and error rate;
6. injected failure/throttle profile and recovery time;
7. pass/fail against the contract without changing thresholds during the run.

External marketplace availability is never included in the TORGNEXA SLO.
Connector remote health, rate limits and freshness remain separate signals.

## Alert / incident handoff

A sustained SLO breach consumes the 30-day error budget and becomes Task-077
incident/runbook input. Warning policies should trigger before 80% saturation or
before the full lag objective is exhausted; paging policy belongs to Task 077.
