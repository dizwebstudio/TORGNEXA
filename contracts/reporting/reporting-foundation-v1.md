# Reporting foundation contract v1

Task 049 establishes a host-owned analytical projection boundary. It is not a
transactional API and it never becomes an authority for commerce writes.

## Source-of-truth invariant

PostgreSQL/domain stores and immutable canonical EventBus facts remain truth.
ClickHouse tables, views and aggregate states are derived, disposable and
rebuildable. A ClickHouse outage may delay reports but must not fail a
transactional PostgreSQL commit or EventBus publication.

## Ingest contract

`internal/platform/reporting.Record` is the canonical ingest unit. Each record
contains a validated EventBus envelope, UTC ingest time, optional source
position, replay identifier and monotonic ingest version. Only explicitly
allow-listed analytical payloads are persisted; all other event payloads are
reduced to `{}` while their non-PII envelope remains available for freshness.

A `Batch`:

- contains 1..5000 records from exactly one organization/workspace;
- contains no duplicate `event_id`;
- has a deterministic SHA-256 deduplication token;
- is acknowledged only after the analytical sink durably accepts it;
- is retried with the same token and exact record set.

`event_id` is the semantic idempotency key. Replay/backfill identifiers are
provenance only and never create a second business fact.

## ClickHouse contract

`deploy/clickhouse/000001_reporting_foundation.sql` owns:

- `event_fact_v1` — replayable event/envelope fact store;
- `ingestion_hourly_state_v1` + materialized view — distinct event count and
  last occurred/ingested timestamps;
- `order_state_v1` + materialized view — latest order state by canonical order
  event version;
- `sales_daily_v1` — daily orders/GMV grouped by original currency;
- `inventory_state_v1` + materialized view — latest exact stock quantity by
  `(occurred_at,event_id)`;
- `inventory_current_v1` and `freshness_v1` query views.

No TTL is hard-coded in Task 049. Task 061 owns policy-driven retention and
tenant deletion across ClickHouse.

## Money/FX invariant

Task 049 never combines amounts of different currencies. Every monetary report
is grouped by the original ISO currency. Target-currency or portfolio totals
remain forbidden until Task 089b persists sourced historical rates and complete
conversion provenance.

## Query contract

The host query surface is `reporting.QueryPort`. Every call requires validated
`tenancy.Scope` containing both organization and workspace. Initial query
families are:

- sales by UTC time range and original currency;
- current inventory by offer/warehouse;
- ingestion freshness by event family.

ClickHouse adapters must bind both tenant identifiers in every query. Returning
cross-tenant rows is a contract violation, not an empty-result optimization.
The public report catalog and report results declare `source=clickhouse`.
Analytical query failure does not fall back to PostgreSQL, avoiding two
user-visible implementations with different freshness and aggregation semantics.

## Replay/backfill contract

`ReplayRunner` pages an authoritative source through a durable checkpoint. A
page that returns neither data nor completion is rejected as stalled. The
checkpoint advances only after every analytical batch in the page is accepted.
Repeating a completed logical replay must not change semantic sales, inventory,
or freshness results.
