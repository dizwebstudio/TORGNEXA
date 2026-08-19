# Task 049: ClickHouse reporting foundation

## Status
`repository-complete` — 2026-08-12.

## Objective
Create event-to-analytics ingestion contracts, core fact tables/materialized views and reporting query ports.

## Dependencies
007, 024

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
PostgreSQL remains truth; replay/backfill tested; freshness metrics exposed.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- `internal/platform/reporting` adds a single-tenant, bounded `Record`/`Batch`/`Sink` ingest contract, deterministic retry tokens, resumable `ReplayRunner`, tenant-scoped `QueryPort` and semantic in-memory reference projection for tests;
- PostgreSQL/EventBus remain authoritative; ClickHouse is explicitly disposable derived state and does not participate in transactional authorization or write acknowledgement;
- analytical payloads are minimized: Task 049 stores full payload only for canonical `commerce.orders.order_changed.v1` and `commerce.inventory.stock_changed.v1`; all other events retain envelope-only freshness evidence;
- `deploy/clickhouse/000001_reporting_foundation.sql` creates replay-safe event, ingestion-freshness, order-state and inventory-state objects plus materialized views and report views;
- repeated event delivery/backfill is semantically idempotent through `event_id`, `uniqExactState` and version/time-keyed `argMaxState` rather than relying on background ReplacingMergeTree merges;
- daily sales stays grouped by original ISO currency and cross-currency totals remain forbidden until Task `089b`;
- freshness exposes distinct event count, last source occurrence, last durable ingest and source lag per event family;
- replay checkpoints advance only after the complete page is durably accepted and a non-progressing source fails closed;
- Docker Compose mounts the ClickHouse init schema; Task 061 still owns retention/tenant deletion and no hard-coded TTL is introduced;
- ADR `0052`, architecture review `ARCH-049`, reporting contract/spec and rebuild runbook are included.

## Qualification
- root tests/vet/build: PASS under the temporary local Go 1.23 compatibility declaration used only for sandbox validation;
- reporting tests including replay/backfill, tenant isolation, original-currency sales, latest inventory and freshness lag: PASS;
- architecture: PASS — `76` modules, `19` providers, `58` reviews, `0` unreviewed changes;
- PostgreSQL migration catalog unchanged at `28`; Task 049 adds ClickHouse-only derived schema;
- canonical nested contract/supply-chain tooling remains environment-blocked by local Go 1.23.2 while repository baseline requires Go >=1.26; canonical `go.mod` is restored before packaging.

Canonical next dependency-ready task: `058 Marketplace Settlement Ledger`.
