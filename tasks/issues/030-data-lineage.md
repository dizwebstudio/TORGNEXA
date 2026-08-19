# Task 030

## Status

Repository implementation: **Completed** on 2026-08-10.

## Objective

Implement immutable cross-domain lineage metadata and timeline reads, starting with canonical Price and Inventory mutations.

## Deliverables

- [x] Provider-neutral lineage model with explicit input refs, output ref, transformation id/version, correlation/causation, result and UTC timestamps.
- [x] Deterministic idempotency-safe lineage record IDs derived from committed event IDs.
- [x] PostgreSQL `lineage_records` + `lineage_inputs` with forced RLS and append-only/no-truncate guards.
- [x] Same-tenant evidence guard linking each lineage record to the exact Task-003 audit row and Task-008 outbox event.
- [x] Atomic Price lineage for create/update, including Offer and prior Price version where applicable.
- [x] Atomic Inventory-position lineage for create/stock mutations, including Offer/Warehouse and prior position version where applicable.
- [x] Bounded read-only timeline query with `(occurred_at,id)` pagination.
- [x] Reusable HTTP timeline handler requiring an authenticated tenant scope resolver rather than caller-supplied tenant IDs.
- [x] Additive OpenAPI timeline operation.
- [x] Draft 2020-12 lineage record/timeline contracts with positive/negative fixtures.
- [x] Migration, architecture, docs and regression checks.

## Boundaries

Task 030 stores provenance references and versions, not copies of business payloads. It does not replace Audit, EventBus, Reconciliation or Approval. Price and Inventory are the first instrumented domains; future PIM/MDM, order status, compliance, EDO, publication, FX and settlement modules reuse the same lineage contract.

## Acceptance

Implementation + tests + docs/contracts; required repository checks pass. Price/stock changes commit domain state, audit evidence, outbox intent and lineage evidence atomically. Timeline reads are tenant-scoped and bounded.
