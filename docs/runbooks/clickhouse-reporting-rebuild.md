# ClickHouse reporting rebuild

This runbook rebuilds Task-049 derived analytical state. It does not restore or
modify PostgreSQL transactional records.

1. Stop report consumers that require complete analytical history. Commerce
   writes and EventBus publication continue.
2. Capture the current canonical PostgreSQL/outbox high-water mark.
3. Recreate the Task-049 ClickHouse schema from
   `deploy/clickhouse/000001_reporting_foundation.sql`.
4. Start a bounded `ReplayRunner` job with a unique operational replay ID and
   durable checkpoint store.
5. Resume the same replay ID after failures. Never skip a failed checkpoint.
6. When complete, compare `freshness_v1` last-occurrence watermarks with the
   captured source high-water mark and validate representative sales/inventory
   rows against PostgreSQL.
7. Resume report consumers. Keep the replay evidence/checkpoint for incident
   audit until Task-061 retention policy allows removal.

Abort and investigate if a replay page makes no checkpoint progress, tenant
scope changes during a job, currency totals are combined across ISO currencies,
or the analytical sink acknowledges before durable persistence.
