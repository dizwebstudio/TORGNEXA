# ADR 0042: Immutable provenance records linked to audit and event evidence

Status: Accepted

## Context

TORGNEXA already records tenant-scoped audit evidence and transactional domain events, but those records do not explain which source facts, mappings, rules or previous versions produced a particular output version. Price and inventory changes need a reproducible provenance path before PIM/MDM, reconciliation, reporting and enterprise integrations expand the number of transformations.

## Decision

Task 030 introduces provider-neutral lineage metadata in `internal/platform/lineage` and PostgreSQL persistence in `internal/platform/postgres/lineagerepo`. A lineage record identifies one output version/field, its ordered input references, transformation identity/version, correlation/causation, source/actor and the exact audit/outbox evidence created by the same mutation.

Lineage stores references and versions, not copies of business payloads. PostgreSQL tables are append-only, forced-RLS and validate that `audit_id` and `event_id` belong to the same organization/workspace. Price and Inventory adapters append lineage after their existing audit and outbox writes inside the same SQL transaction.

A bounded read-only timeline API exposes records for one authenticated tenant/entity/field. Tenant scope is supplied by an injected authenticated resolver rather than caller-controlled organization/workspace parameters.

## Consequences

Price and stock provenance is durable and queryable without log scraping. Future PIM, mapping/rule, order-status, compliance, EDO and publication modules can reuse the same contract. Historical lineage survives binary rollback and can link directly to actor audit evidence and integration event intent.

## Alternatives considered

Reconstructing lineage from Kafka was rejected because publication is at-least-once and event payloads are not a complete transformation graph. Extending `audit_records.summary` was rejected because audit and lineage answer different questions and arbitrary provenance blobs would weaken schema/retention discipline. Provider-specific lineage columns were rejected because they would contaminate canonical Core models. Copying entire before/after payloads was rejected for privacy, storage and contract-drift reasons.

## Compatibility impact

The change is additive. Existing Price/Inventory Core interfaces and published event schemas are unchanged. Two new Draft 2020-12 lineage schemas and one additive OpenAPI timeline operation are introduced. Existing API operations remain unchanged.

## Migration and data impact

Expand migration `000014_data_lineage.sql` adds `lineage_records` and `lineage_inputs`, indexes, forced RLS and append-only/evidence triggers. No existing table or column is dropped or renamed. Lineage rows reference existing audit/outbox ids and must be retained as historical evidence.

## Security and privacy impact

Lineage contains bounded identifiers, versions, mapping/rule ids and timestamps only; it does not store credentials or business payload copies. Forced RLS and same-tenant evidence guards prevent cross-tenant reads or links. The HTTP timeline requires authenticated scope resolution and does not trust tenant ids from query/header input.

## Operational impact

Operators and support tooling gain a deterministic timeline for price/stock changes and can correlate each item with the audit and outbox records. Timeline reads use bounded pagination and an index ordered by occurrence time/id. Rollback keeps the evidence tables in place; future retention policies must treat lineage as auditable provenance rather than disposable debug logging.
