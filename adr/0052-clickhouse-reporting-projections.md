# ADR 0052: Replay-safe ClickHouse reporting projections

Status: Accepted

## Context

TORGNEXA requires low-latency analytical history without allowing an analytical
store to become a second transactional authority. Event delivery and disaster
replay are at-least-once, while ClickHouse `ReplacingMergeTree` removes rows
only during background merges. Financial reporting also cannot combine
currencies before sourced historical FX conversion exists.

## Decision

Keep PostgreSQL/domain stores authoritative and project canonical EventBus facts
asynchronously into ClickHouse. Use a bounded provider-neutral Go ingest/query
port and minimized analytical payloads. The raw analytical table uses
`ReplacingMergeTree` for storage cleanup, while user-facing state is made
replay-safe independently through aggregate-state materialized views:
`uniqExact(event_id)` for ingest metrics, `argMax(event_version)` for orders and
`argMax(occurred_at,event_id)` for inventory.

Every batch is single-tenant and has a deterministic retry token. Replay pages
advance a durable checkpoint only after sink acknowledgement. Report queries
must bind organization and workspace. Monetary results remain grouped by source
currency until Task 089b.

Task 049 deliberately defines no ClickHouse TTL. Task 061 owns coordinated
retention/deletion policy across analytical and operational stores.

## Consequences

ClickHouse can be dropped and rebuilt without blocking PostgreSQL recovery.
Repeated Kafka delivery or a full analytical replay does not inflate the
initial business-facing metrics. Query latency remains suitable for BI while
transactional write availability is decoupled from ClickHouse health.

The design retains some duplicate raw physical rows until merges occur and
requires explicit aggregate-state queries/views. New analytical payloads must
be admitted deliberately rather than copying every event body into ClickHouse.

## Security and privacy impact

Only allow-listed fact payloads are retained; all other events contribute only
bounded envelope fields needed for freshness. Tenant scope is mandatory on
batches and report queries. ClickHouse is never consulted for authorization or
transactional decisions.

## Operational impact

Deployments must use durable insert acknowledgement and deterministic retry.
Backfill/rebuild uses explicit checkpoints and freshness watermarks. Task 066
will put SLO thresholds on the freshness metrics; Task 077 will operationalize
incident response.

## Alternatives considered

Using ClickHouse as a transactional read/write authority was rejected because it
would couple commerce availability and authorization to an analytical store.
Plain `ReplacingMergeTree` queries were rejected as the only deduplication
mechanism because replacement is asynchronous. Summing raw event deltas was
rejected because replay could inflate financial metrics. Persisting every event
payload was rejected because it would unnecessarily broaden privacy exposure.

## Compatibility impact

No public REST/OpenAPI, Connector SDK or EventBus schema changes are introduced.
The new Go reporting port is additive and host-only. Existing PostgreSQL/domain
write paths and connector providers are unchanged.

## Migration and data impact

Task 049 adds only derived ClickHouse objects. It intentionally adds no
PostgreSQL migration and no hard-coded ClickHouse TTL. Existing operational data
is unchanged; analytical state can be rebuilt from authoritative history.
