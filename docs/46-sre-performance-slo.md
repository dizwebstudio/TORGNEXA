# SRE, Performance & SLO Contracts

Performance is an explicit contract, not an afterthought.

## SLO dimensions

- API availability and latency by endpoint class;
- event processing lag;
- connector sync freshness;
- reconciliation completion time;
- publication scheduling delay;
- webhook delivery success/latency;
- reporting freshness;
- error budget and incident thresholds.

## Testing

Maintain deterministic load profiles for API, Kafka consumers, bulk price/stock updates, import/export, reconciliation, webhook fanout and reporting. Test remote throttling and partial outages. Capacity results must state dataset size, concurrency and hardware.

No SLO should promise availability of an external marketplace; instead expose connector freshness/health separately.

## Task 066 executable baseline

Task 066 is repository-complete through `internal/platform/slo`,
`contracts/operations/slo-v1.md` and `performance/baseline-v1.json`.
`make performance` verifies the deterministic load/failure profiles and their
p50/p95/p99, throughput, lag and saturation results. See
`docs/operations/066-slo-performance-test-suite.md` for deployment evidence
requirements and the boundary between normalized CI qualification and real
capacity testing.
