# Task 049 — ClickHouse reporting foundation

## Architecture

ClickHouse is a read-optimized history/projection store. PostgreSQL remains the
operational system of record and Kafka/EventBus remains the durable event
transport. Transactional paths do not synchronously depend on ClickHouse.

The Go boundary is `internal/platform/reporting`:

- `Ingestor` converts canonical EventBus facts into bounded analytical batches;
- `Sink` is the ClickHouse-write port;
- `ReplayRunner` reconstructs projections from authoritative history with a
  durable checkpoint;
- `QueryPort` exposes tenant-scoped sales, inventory and freshness reads;
- `MemoryProjection` is a semantic reference for tests only, not production
  storage.

## Why aggregate-state materialized views

Raw analytics facts use `ReplacingMergeTree`, but ClickHouse documents that
replacement occurs during background merges and therefore cannot itself be
used as a uniqueness guarantee at query time. Task 049 consequently makes the
business-facing projections replay-safe independently:

- ingestion counts use `uniqExactState(event_id)`;
- order state uses `argMaxState(..., version)`;
- inventory uses `argMaxState(..., tuple(occurred_at,event_id))`.

This makes a repeated event replay semantically idempotent before background
part merging finishes.

For synchronous insert retries, production adapters must reuse the exact batch
and its deterministic `DedupToken`. If asynchronous inserts are chosen by a
deployment, acknowledgements must wait for persistence; fire-and-forget mode is
outside the Task-049 contract.

## Payload minimization

All events contribute envelope-only freshness data. Only these payloads are
currently retained because Task 049 owns their analytical semantics:

- `commerce.orders.order_changed.v1`;
- `commerce.inventory.stock_changed.v1`.

Every other payload becomes `{}` before reaching the analytical sink. Future
reporting tasks must explicitly extend this allow-list with privacy and
architecture review.

## Initial reports

### Sales

`sales_daily_v1` reports order count, fulfilled/cancelled count and gross minor
units. Results are grouped by original currency. Cancelled orders are excluded
from gross minor units. After Task 089b, `reporting.ConvertSalesBucket` may derive
one explicit target-currency bucket at a caller-selected UTC `as_of`; the result
must retain its immutable FX conversion-record ID. Raw ClickHouse facts are never
rewritten into a presentation currency.

### Inventory

`inventory_current_v1` gives current exact decimal quantity per offer and
warehouse. Quantity is retained as exact decimal text rather than binary float.

### Freshness

`freshness_v1` exposes, per organization/workspace/event family:

- distinct ingested event count;
- last source event occurrence time;
- last durable ingest time;
- source lag in seconds.

These are the Task-049 freshness metrics consumed later by Task 066 SLOs and
Task 077 incident/runbook automation.

## Backfill and recovery

Use an authoritative PostgreSQL/outbox archive reader behind `ReplaySource`.
Run bounded pages under a stable `replay_id`; persist the returned checkpoint
only after the sink acknowledges the whole page. A failed page is repeated from
its previous checkpoint. `event_id`, not replay ID, owns semantic uniqueness.

After ClickHouse loss:

1. recreate `000001_reporting_foundation.sql`;
2. choose the authoritative reconstruction boundary;
3. run replay pages to completion;
4. compare freshness watermarks and source counts;
5. only then re-enable report consumers that require complete history.

## Production adapter requirements

The production adapter uses ClickHouse's HTTP protocol and must:

- use durable acknowledgement semantics;
- send bounded batches (client-side batching is preferred for normal ingest);
- reuse the deterministic dedup token on retries;
- bind organization + workspace on every query;
- apply query deadlines and bounded result sizes;
- never log credentials, raw query auth material or disallowed payloads;
- expose insert/query failure and freshness lag to the host metrics layer.

API queries use typed ClickHouse parameters for both `organization_id` and
`workspace_id`, a bounded response body and a per-query deadline. Optional
credentials are carried only in `X-ClickHouse-User` and `X-ClickHouse-Key`
headers. They are never placed in the endpoint URL or propagated into errors.
The endpoint is configured by `CLICKHOUSE_DSN`; query credentials use
`CLICKHOUSE_USERNAME` and `CLICKHOUSE_PASSWORD`, and the deadline uses
`TORGNEXA_CLICKHOUSE_QUERY_TIMEOUT`.
Community initialization creates a dedicated `torgnexa` ClickHouse user and a
random password shared only through the private `.env`; the network-facing HTTP
adapter does not rely on the image's localhost-only default user.

## Report exports

`GET /api/v1/reports/{report_id}` accepts `format=json`, `csv`, or `pdf`.
CSV and PDF use the same authenticated tenant scope, ClickHouse query, filters
and 200-row bound as the interactive report; export never performs an
unscoped second query. The API generates PDF bytes in memory with an embedded
Unicode font, repeats table headings across landscape A4 pages, and returns
`application/pdf` plus an attachment filename. It does not persist generated
files or invoke a browser/system print dialog. Responses are marked `no-store`.
