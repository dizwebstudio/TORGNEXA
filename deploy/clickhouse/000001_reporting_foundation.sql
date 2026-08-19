-- TORGNEXA Task 049: ClickHouse reporting foundation.
-- PostgreSQL/event stores remain authoritative. These objects are disposable,
-- replayable analytical projections and MUST NOT be used to authorize writes.

CREATE DATABASE IF NOT EXISTS torgnexa_reporting;

CREATE TABLE IF NOT EXISTS torgnexa_reporting.event_fact_v1
(
    event_id String,
    event_type LowCardinality(String),
    occurred_at DateTime64(6, 'UTC'),
    ingested_at DateTime64(6, 'UTC'),
    organization_id String,
    workspace_id String,
    entity_type LowCardinality(String),
    entity_id String,
    source LowCardinality(String),
    correlation_id String,
    causation_id String,
    actor_id String,
    trace_id String,
    analytics_data_json String,
    replay_id String,
    source_stream LowCardinality(String),
    source_partition Int32,
    source_offset Int64,
    ingest_version UInt64
)
ENGINE = ReplacingMergeTree(ingest_version)
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (organization_id, workspace_id, event_id)
SETTINGS index_granularity = 8192;

-- Replay-safe hourly ingestion states. uniqExact(event_id) prevents a repeated
-- backfill from inflating counts even before ReplacingMergeTree parts merge.
CREATE TABLE IF NOT EXISTS torgnexa_reporting.ingestion_hourly_state_v1
(
    hour DateTime('UTC'),
    organization_id String,
    workspace_id String,
    event_family LowCardinality(String),
    event_count AggregateFunction(uniqExact, String),
    last_occurred_at AggregateFunction(max, DateTime64(6, 'UTC')),
    last_ingested_at AggregateFunction(max, DateTime64(6, 'UTC'))
)
ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(hour)
ORDER BY (organization_id, workspace_id, event_family, hour);

CREATE MATERIALIZED VIEW IF NOT EXISTS torgnexa_reporting.ingestion_hourly_mv_v1
TO torgnexa_reporting.ingestion_hourly_state_v1
AS
SELECT
    toStartOfHour(ingested_at) AS hour,
    organization_id,
    workspace_id,
    arrayStringConcat(arraySlice(splitByChar('.', event_type), 1, 2), '.') AS event_family,
    uniqExactState(event_id) AS event_count,
    maxState(occurred_at) AS last_occurred_at,
    maxState(ingested_at) AS last_ingested_at
FROM torgnexa_reporting.event_fact_v1
GROUP BY hour, organization_id, workspace_id, event_family;

-- Latest order state is derived only from the canonical order_changed v1 event.
-- argMax(version) makes duplicate replay blocks semantically idempotent.
CREATE TABLE IF NOT EXISTS torgnexa_reporting.order_state_v1
(
    organization_id String,
    workspace_id String,
    order_id String,
    status AggregateFunction(argMax, String, Tuple(UInt64, String)),
    currency AggregateFunction(argMax, String, Tuple(UInt64, String)),
    total_minor AggregateFunction(argMax, Int64, Tuple(UInt64, String)),
    first_seen_at AggregateFunction(min, DateTime64(6, 'UTC')),
    last_changed_at AggregateFunction(argMax, DateTime64(6, 'UTC'), Tuple(UInt64, String)),
    last_event_id AggregateFunction(argMax, String, Tuple(UInt64, String))
)
ENGINE = AggregatingMergeTree()
PARTITION BY sipHash64(organization_id, workspace_id) % 32
ORDER BY (organization_id, workspace_id, order_id);

CREATE MATERIALIZED VIEW IF NOT EXISTS torgnexa_reporting.order_state_mv_v1
TO torgnexa_reporting.order_state_v1
AS
SELECT
    organization_id,
    workspace_id,
    JSONExtractString(analytics_data_json, 'order_id') AS order_id,
    argMaxState(JSONExtractString(analytics_data_json, 'status'), tuple(toUInt64(JSONExtractUInt(analytics_data_json, 'version')), event_id)) AS status,
    argMaxState(JSONExtractString(analytics_data_json, 'total', 'currency'), tuple(toUInt64(JSONExtractUInt(analytics_data_json, 'version')), event_id)) AS currency,
    argMaxState(JSONExtractInt(analytics_data_json, 'total', 'minor_units'), tuple(toUInt64(JSONExtractUInt(analytics_data_json, 'version')), event_id)) AS total_minor,
    minState(occurred_at) AS first_seen_at,
    argMaxState(occurred_at, tuple(toUInt64(JSONExtractUInt(analytics_data_json, 'version')), event_id)) AS last_changed_at,
    argMaxState(event_id, tuple(toUInt64(JSONExtractUInt(analytics_data_json, 'version')), event_id)) AS last_event_id
FROM torgnexa_reporting.event_fact_v1
WHERE event_type = 'commerce.orders.order_changed.v1'
  AND JSONExtractString(analytics_data_json, 'order_id') != ''
  AND JSONExtractUInt(analytics_data_json, 'version') > 0
GROUP BY organization_id, workspace_id, order_id;

-- Sales stays grouped by original currency. Cross-currency totals are forbidden
-- until Task 089b provides persisted historical FX facts and provenance.
CREATE VIEW IF NOT EXISTS torgnexa_reporting.sales_daily_v1 AS
SELECT
    toDate(first_seen_at) AS day,
    organization_id,
    workspace_id,
    currency,
    count() AS orders,
    countIf(status = 'fulfilled') AS fulfilled_orders,
    countIf(status = 'cancelled') AS cancelled_orders,
    sumIf(total_minor, status != 'cancelled') AS gross_minor_units
FROM
(
    SELECT
        organization_id,
        workspace_id,
        order_id,
        argMaxMerge(status) AS status,
        argMaxMerge(currency) AS currency,
        argMaxMerge(total_minor) AS total_minor,
        minMerge(first_seen_at) AS first_seen_at
    FROM torgnexa_reporting.order_state_v1
    GROUP BY organization_id, workspace_id, order_id
)
GROUP BY day, organization_id, workspace_id, currency;

-- Current stock is replay-safe by latest (occurred_at,event_id) tuple. Quantity
-- remains exact decimal text because stock units can have domain-specific scale.
CREATE TABLE IF NOT EXISTS torgnexa_reporting.inventory_state_v1
(
    organization_id String,
    workspace_id String,
    offer_id String,
    warehouse_id String,
    quantity AggregateFunction(argMax, String, Tuple(DateTime64(6, 'UTC'), String)),
    changed_at AggregateFunction(argMax, DateTime64(6, 'UTC'), Tuple(DateTime64(6, 'UTC'), String)),
    event_id AggregateFunction(argMax, String, Tuple(DateTime64(6, 'UTC'), String))
)
ENGINE = AggregatingMergeTree()
PARTITION BY sipHash64(organization_id, workspace_id) % 32
ORDER BY (organization_id, workspace_id, offer_id, warehouse_id);

CREATE MATERIALIZED VIEW IF NOT EXISTS torgnexa_reporting.inventory_state_mv_v1
TO torgnexa_reporting.inventory_state_v1
AS
SELECT
    organization_id,
    workspace_id,
    JSONExtractString(analytics_data_json, 'offer_id') AS offer_id,
    JSONExtractString(analytics_data_json, 'warehouse_id') AS warehouse_id,
    argMaxState(JSONExtractString(analytics_data_json, 'new_quantity'), tuple(occurred_at, torgnexa_reporting.event_fact_v1.event_id)) AS quantity,
    argMaxState(occurred_at, tuple(occurred_at, torgnexa_reporting.event_fact_v1.event_id)) AS changed_at,
    argMaxState(torgnexa_reporting.event_fact_v1.event_id, tuple(occurred_at, torgnexa_reporting.event_fact_v1.event_id)) AS event_id
FROM torgnexa_reporting.event_fact_v1
WHERE event_type = 'commerce.inventory.stock_changed.v1'
  AND JSONExtractString(analytics_data_json, 'offer_id') != ''
  AND JSONExtractString(analytics_data_json, 'warehouse_id') != ''
GROUP BY organization_id, workspace_id, offer_id, warehouse_id;

CREATE VIEW IF NOT EXISTS torgnexa_reporting.inventory_current_v1 AS
SELECT
    organization_id,
    workspace_id,
    offer_id,
    warehouse_id,
    argMaxMerge(quantity) AS quantity,
    argMaxMerge(changed_at) AS changed_at,
    argMaxMerge(event_id) AS event_id
FROM torgnexa_reporting.inventory_state_v1
GROUP BY organization_id, workspace_id, offer_id, warehouse_id;

CREATE VIEW IF NOT EXISTS torgnexa_reporting.freshness_v1 AS
SELECT
    organization_id,
    workspace_id,
    event_family,
    uniqExactMerge(torgnexa_reporting.ingestion_hourly_state_v1.event_count) AS event_count,
    maxMerge(torgnexa_reporting.ingestion_hourly_state_v1.last_occurred_at) AS last_occurred_at,
    maxMerge(torgnexa_reporting.ingestion_hourly_state_v1.last_ingested_at) AS last_ingested_at,
    greatest(0, dateDiff('second', maxMerge(torgnexa_reporting.ingestion_hourly_state_v1.last_occurred_at), maxMerge(torgnexa_reporting.ingestion_hourly_state_v1.last_ingested_at))) AS source_lag_seconds,
    now64(6, 'UTC') AS observed_at
FROM torgnexa_reporting.ingestion_hourly_state_v1
GROUP BY organization_id, workspace_id, event_family;
