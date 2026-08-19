# Task 023

## Status

Repository implementation: **Completed** on 2026-08-10.

## Objective

Implement canonical Brand/Category/Attribute master data, product master-data assignments, provider-neutral external mappings, explainable duplicate detection, deterministic merge preview, and field-authority rules without provider-specific category/attribute fields in Core.

## Deliverables

- [x] Canonical tenant-scoped Brand, hierarchical Category and typed Attribute definitions.
- [x] Product↔Brand, Product↔Category and typed Product Attribute Value persistence against Task-004 Product IDs.
- [x] Exact decimal attribute encoding as JSON strings; no floating-point PIM amount/measurement drift.
- [x] Generic connector mapping v3 extended additively to Brand/Category/Attribute identities.
- [x] Per-entity/field/source authority rules with bounded deterministic priority.
- [x] Explainable duplicate candidates with bounded score/signals and review lifecycle.
- [x] Deterministic merge preview with explicit keep/take/equal/conflict decisions and SHA-256 fingerprint.
- [x] Merge preview is non-executing; equal-authority conflicts remain unresolved until reviewed.
- [x] Forced-RLS PostgreSQL storage, composite tenant FKs, immutable identity/lifecycle/type guards and no hard-delete/truncate paths.
- [x] PIM mutation evidence committed atomically through Task-003 Audit, Task-008 Outbox and Task-030 Lineage.
- [x] Draft 2020-12 Brand/Category/Attribute/authority/duplicate/merge/mapping/event contracts and fixtures.
- [x] Migration, architecture, docs and repository regression checks.

## Boundaries

Task 004 remains the canonical Product/Offer aggregate. Task 023 attaches master-data relationships rather than adding provider taxonomy fields to Product/Offer. Task 023 creates and stores merge previews but does not apply a destructive merge. Sensitive merge/publication writes remain subject to Task 017 approval. Task 013/014 later own synchronization/reconciliation execution against these canonical mappings.

## Acceptance

Implementation + tests + docs/contracts; required repository checks pass. Category/Brand/Attribute external mappings are provider-neutral, duplicate evidence is explainable/reviewable, field authority is explicit, and merge preview cannot silently overwrite equal-authority conflicts.
