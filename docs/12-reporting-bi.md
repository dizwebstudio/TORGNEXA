# Reporting & BI

ClickHouse is the analytical/history store; PostgreSQL remains operational truth.

Report families:
- sales/GMV/net sales;
- P&L and unit economics;
- marketplace settlement/payment reconciliation;
- commissions/logistics/storage/returns/penalties/compensation;
- advertising/promotions and attributable social/channel performance;
- inventory velocity, days-of-supply, dead/overstock/stockout risk;
- procurement/replenishment and WMS productivity;
- customer service/claims/SLA;
- compliance/fiscal/EDO/signing status;
- connector/sync/reconciliation health.

Reports must distinguish source facts, normalized ledger facts and derived/attributed metrics. Every derived metric has definition/version/source lineage. Kafka replay/backfill is supported for analytical reconstruction.

## Task 049 foundation

The initial implementation is `internal/platform/reporting` plus
`deploy/clickhouse/000001_reporting_foundation.sql`. Ingest batches are
single-tenant, replay-safe and acknowledged only after durable analytical sink
acceptance. ClickHouse contains derived event/order/inventory/freshness state;
it never authorizes or confirms transactional writes.

Initial ClickHouse monetary facts remain grouped by original currency. Task 089b
now permits explicit historical cross-currency derivation only through a persisted
FX conversion record with caller-selected UTC `as_of`; silent aggregation across
currencies remains forbidden. Freshness exposes per-event-family last occurrence,
last ingest, distinct event count and lag. See
`docs/reporting/049-clickhouse-foundation.md` and ADR 0052.

The production `/api/v1/reports` read path uses the bounded ClickHouse HTTP
adapter behind `reporting.QueryPort`. The report catalog and every returned
dataset identify `source=clickhouse`; PostgreSQL is no longer a user-facing
report fallback. An unavailable analytical store fails only the report request
and does not affect transactional API writes.
