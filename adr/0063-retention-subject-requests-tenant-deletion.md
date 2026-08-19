# ADR 0063: Retention, subject requests and tenant deletion

## Status
Accepted

## Context
Task 061 must execute Task-060 privacy metadata across authoritative and derived stores without turning a derived index, cache, object store or ClickHouse projection into a source of truth. Privacy operations may span many stores and therefore cannot rely on a single atomic database transaction.

## Decision
Use a tenant-scoped resumable workflow coordinator. Every workflow is decomposed into named store targets with versioned cursors. A cursor advances only after the target page succeeds and append-only evidence is durably recorded. Store adapters receive opaque subject references and released correction-artifact references rather than raw PII.

Subject export/access, correction, deletion and restriction share the same orchestration boundary. Retention expiry maps Task-060 dispositions to delete, anonymize or archive-then-delete. `manual_review` is fail-closed and produces a blocked workflow with no store mutation. Tenant deletion uses the same target model and requires at least one authoritative target before it may start.

Destructive workflows evaluate scoped legal holds before any target call. Legal holds are immutable except for an explicit release transition. Task-060 `legal_hold_permitted=false` means an ordinary retention-expiry policy is not overridden by a hold; subject deletion and tenant deletion are hold-aware by default.

## Alternatives considered
A monolithic delete transaction was rejected because PostgreSQL, ClickHouse, search/cache and object storage do not share a transaction boundary. Best-effort fan-out without durable cursors was rejected because retries could skip or duplicate work. Passing raw subject data through the coordinator was rejected because it would unnecessarily widen the PII boundary.

## Compatibility impact
Additive platform capability and PostgreSQL schema only. Existing public API, Connector SDK and provider contracts are unchanged.

## Migration and data impact
Migration `000039_retention_subject_requests_tenant_deletion.sql` adds RLS-protected request/job/target/hold/evidence tables. Evidence is append-only. Existing readers and writers remain valid.

## Security and privacy impact
The coordinator stores only opaque subject identifiers, purpose/class metadata, cursors, counts, hashes and released artifact references. It never stores correction/export payload bytes. Legal-hold and workflow lifecycle transitions emit legally-significant audit records.

## Operational impact
Workers may call `Advance` repeatedly with bounded page counts. Target errors leave the durable cursor unchanged, allowing safe resume. Derived stores are explicit targets, so deletion completion cannot be reported until every configured target completes.

## Consequences
Every production store containing Task-060 governed data must expose a Task-061 store adapter. Deployment qualification must include the real PostgreSQL, ClickHouse, object-storage and search/cache adapters for the selected topology.
