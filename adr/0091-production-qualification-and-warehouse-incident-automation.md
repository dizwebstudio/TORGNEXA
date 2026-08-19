# ADR-0091: Production qualification and warehouse incident automation

Status: Accepted

## Context

Tasks 113-115 assembled the production worker, connector reconciliation, notifications/privacy execution and persistent warehouse operational state. Two gaps remained. First, `UNAVAILABLE`/`LOST` state prevented unsafe new reservations but did not itself drive a durable, resumable incident workflow. Second, Task 066 defined deterministic SLO objectives but repository simulation cannot prove a deployed PostgreSQL/Kafka/worker topology survives broker/database restarts, duplicate delivery, or real black-box load.

Provider write expansion has the same qualification problem: a capability must not be advertised merely because a provider has some remote write API. The canonical Connector SDK must be able to express the exact desired state and retry semantics without provider-specific fields leaking into Core.

## Decision

Warehouse hard-down transitions create a durable `warehouse_incidents` record. The worker claims it through the tenant-safe dispatcher and processes bounded offer batches. It records a route only when an enabled route points to an operational warehouse with positive ATP for the same offer. Decisions are evidence only; no stock row moves or appears. Any unroutable offer makes the incident terminate as `needs_attention`.

Deployment qualification is an executable gate separate from repository SLO regression. The normal application image inserts a transactional-outbox event, waits for publication and the immutable Inbox receipt, redelivers the same ID, publishes a marker, and requires one receipt. It also opens a synthetic `LOST` warehouse incident with a positive-ATP backup, requires routed evidence, and proves source physical stock is unchanged. The shell gate repeats this after worker, Kafka and PostgreSQL restart drills and applies Task-066 API availability/p99/throughput limits. Evidence is generated per run, never fabricated in the repository.

Yandex Market gains only `prices.write`. The provider-neutral `PriceWriteRequest` maps faithfully to the documented business-wide and campaign-specific exact-price endpoints. RUB is translated to the provider's RUR code, crossed-out price is restricted to the provider's integer contract, transport ambiguity remains retryable, and successful remote acceptance is not falsely marked read-after-write reconciled because Market documents eventual catalogue propagation. Wildberries/Ozon remain read-only in this change because the current generic write contracts cannot yet represent their required warehouse/listing-specific semantics without broadening the SDK.

## Compatibility impact

The warehouse/API additions and Yandex `prices.write` declaration are additive. Existing provider-neutral SDK types and event schemas remain unchanged.

## Migration and data impact

Migration 000073 is expand-only and adds incident/job evidence; it never moves or creates inventory. Migration 000072 remains the allocation safety guard during mixed-version rollout.

## Operational impact

A warehouse outage can now progress automatically and resumably while retaining offers that need operator attention. Deployment qualification is a separate Docker-backed release gate and emits retained evidence per exact topology.

## Security and privacy impact

All incident tables use tenant scope and FORCE RLS. Qualification data is synthetic and evidence excludes secret values and customer PII. Worker business processing remains tenant-scoped after narrow job claiming.

## Consequences

A warehouse outage can now progress automatically and resumably while preserving physical truth. Operations receive a persistent list of offers that could not be safely rerouted. Runtime qualification is reproducible and fail-closed, but it deliberately requires Docker and the exact target topology; a source-tree check cannot substitute for retained deployment evidence.

The qualified write surface grows more slowly than the number of remote APIs. This is intentional: capability truth is more important than apparent breadth.

## Rollout notes

Migration 000073 is expand-only. Mixed-version binaries that know migration 72 but not 73 remain safe because migration 72 already blocks reservation increases for unavailable/lost warehouses; incident automation begins after the new schema is present. Worker dispatch treats the new kind as temporarily unavailable during rolling schema rollout. The warehouse API additions and Yandex `prices.write` declaration are additive.

## Additional security notes

All incident tables are tenant-scoped with FORCE ROW LEVEL SECURITY. Worker claims are narrow SECURITY DEFINER dispatch operations; business reads return to normal tenant scope. Qualification data uses a synthetic dedicated tenant and contains no customer PII or credentials. Generated qualification evidence records timings/topology metadata, not secret values.

## Alternatives considered

Automatically copying source warehouse quantities to a fallback warehouse was rejected because a lost warehouse's physical stock cannot be assumed to exist elsewhere. In-memory incident queues were rejected because process restart would lose recovery state. Marking every marketplace as write-capable was rejected because provider API existence alone does not satisfy canonical desired-state, idempotency, approval and reconciliation guarantees.
