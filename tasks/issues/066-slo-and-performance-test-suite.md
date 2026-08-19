# Task 066: SLO and performance test suite

## Status
`repository-complete` — 2026-08-12.

## Objective
Define SLIs/SLOs and deterministic load/failure profiles for API/Kafka/sync/webhooks/reporting.

## Dependencies
014, 063, 049

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Baseline p50/p95/p99, throughput/lag/saturation recorded with thresholds.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- `internal/platform/slo` defines executable API/Kafka/sync/webhook/reporting objectives and fail-closed evaluation of availability, nearest-rank p50/p95/p99, throughput, lag and saturation;
- the error-budget window is 30 days and external marketplace availability is explicitly excluded from TORGNEXA host availability promises;
- deterministic repository profiles cover API steady/burst, Kafka throttling, sync partial outage, webhook partial outage and a slow reporting sink;
- `performance/baseline-v1.json` records every profile's p50/p95/p99, throughput, lag and saturation next to the threshold used for admission;
- `scripts/check-performance-slo.sh` regenerates and diffs the baseline, so workload/threshold drift fails closed in `make check`;
- deployment qualification is separate and must record topology, hardware, dataset, concurrency, duration and exact software/image versions before production;
- ADR `0064`, `ARCH-066`, SLO contract and operator documentation are included.

## Qualification
- Task-066 unit/baseline tests: PASS;
- deterministic profile baseline: PASS for all six profiles; repeat regeneration 20/20 PASS;
- architecture: PASS — `89` modules, `19` providers, `70` reviews, `0` unreviewed changes;
- migrations: PASS — `39/39`, latest `000039`; Task 066 adds no migration;
- SDK drift/boundary: PASS — `30` public operations, OpenAPI `0.7.1`;
- no public API/event/Connector SDK/database schema compatibility change.

Deployment follow-up: repeat the same objectives against the exact production topology with real OpenTelemetry/metrics observations. Task `077` binds sustained breaches/error-budget burn to executable incident runbooks and paging policy.
