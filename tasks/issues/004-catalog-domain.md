# Task 004 — Catalog Domain

Status: repository-completed (2026-08-09)

Canonical Product/Offer domain and repository ports. Remote mappings only through connector mapping abstraction.

## Acceptance

- Canonical provider-neutral `Product` and `Offer` aggregates exist with tenant ownership, stable local identifiers, optimistic versions, UTC persistence metadata and forward-only `draft -> active -> archived` lifecycle.
- Product `code` and Offer `sku` are immutable canonical identifiers. Optional GTIN accepts only GTIN-8/12/13/14 with a valid GS1 modulo-10 check digit.
- Offer activation fails unless the parent Product is active. Product archival fails while any non-archived Offer remains.
- Price, inventory, provider listing state, provider category/attribute fields and remote IDs are absent from the Product/Offer core; Tasks 005/023 own those concerns.
- `catalog.Repository` exposes tenant-scoped Product/Offer lookups and mutating commands with optimistic expected versions.
- Every Product/Offer mutation commits its canonical versioned event intent through Task-008 Transactional Outbox in the same PostgreSQL transaction.
- Remote identity is isolated behind `connectors.MappingRepository` and `connector_entity_mappings`; Product/Offer rows contain no provider-specific ID columns or provider branching.
- Migration `000009_catalog_domain.sql` provides composite tenant FKs, forced RLS, lifecycle/identity/GTIN defense-in-depth, mapping integrity and no application hard-delete path.
- Product, Offer, mapping and catalog-event Draft 2020-12 contracts/fixtures are registered; database/backup/PITR rehearsals cover catalog RLS and atomic rollback.
- Unit, migration, contract, architecture and full repository checks pass at the available repository-validation level.

## Follow-up boundary

Task 005 adds money-safe Price and InventoryPosition against canonical Offer IDs. Task 006 later adds Order mappings through the same generic connector-mapping concept. Task 023 expands PIM/MDM with Brand/Category/Attribute mapping and merge/deduplication primitives.
