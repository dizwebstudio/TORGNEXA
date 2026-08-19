# SLO contract v1

Task 066 defines host-owned service objectives for five critical paths. External
marketplace availability is not part of a TORGNEXA availability promise; remote
health/throttling is observed as connector freshness and dependency state.

| Path | Availability | p50 | p95 | p99 | Min throughput | Max lag | Max saturation |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| API | 99.9% | 100 ms | 300 ms | 750 ms | 250 ops/s/replica | 0 | 80% |
| Kafka/event processing | 99.95% | 50 ms | 250 ms | 1 s | 1000 events/s/consumer-set | 30 s | 80% |
| Sync/reconciliation worker | 99.5% | 500 ms | 2 s | 5 s | 20 items/s/worker-set | 5 min | 80% |
| Webhook dispatcher | 99.9% | 250 ms | 2 s | 10 s | 50 deliveries/s/worker-set | 60 s | 80% |
| Reporting | 99.9% | 300 ms | 2 s | 5 s | 50 queries/s/replica | 60 s freshness | 80% |

The error-budget window is 30 days. Availability is successful host-owned
operations divided by total eligible operations; caller validation failures and
external-provider downtime are not counted as host availability failures when
they are correctly classified.

Percentiles use nearest-rank p50/p95/p99 over eligible successful/failed host
operations in the same path and window. Throughput is completed eligible
operations divided by elapsed observation time. Lag is the oldest unprocessed
or unfresh age relevant to the path. Saturation is busy capacity / configured
capacity for the constraining worker/pool/partition set.

A release qualification fails closed if any objective is breached. Production
capacity evidence must identify topology, dataset, concurrency, duration and
software/hardware versions; repository-normalized baselines are not production
capacity claims.
