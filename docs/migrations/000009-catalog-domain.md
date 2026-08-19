# Migration 000009 — canonical catalog domain

`000009_catalog_domain.sql` is the high-risk additive expand migration for Task 004.

## Canonical ownership

`products` is the descriptive master and `offers` is the sellable variation. Neither table contains a provider name, remote identifier, price, inventory quantity, publication state, or provider-specific category/attribute field. Price/inventory remain Task 005; PIM category/brand/attribute mapping remains Task 023.

Product identity is `(tenant, id)` with immutable `code`; Offer identity is `(tenant, id)` with immutable `product_id` and `sku`. Both start at `draft` / version `1`, then use forward-only `draft -> active -> archived` lifecycle and optimistic monotonic versions. PostgreSQL insert guards enforce the initial state as well as update guards enforcing each transition. An active Offer requires an active Product, and a Product cannot archive while any Offer remains non-archived.

GTIN is optional but, when present, is digit-only GTIN-8/12/13/14 and must pass the GS1 modulo-10 check digit in both runtime and PostgreSQL.

## Connector mapping boundary

`connector_entity_mappings` is the only persistence bridge between a canonical local entity and a connector-account remote ID. Task 004 admits `product` and `offer`; future domain migrations may expand the generic entity type. Composite connector-account ownership prevents mixed-tenant mapping rows, and a trigger verifies the referenced local Product/Offer exists in that tenant.

Provider-specific IDs must not be added to `products` or `offers`.

## Events and atomicity

Catalog repository mutations enqueue `commerce.catalog.product_changed.v1` or `commerce.catalog.offer_changed.v1` through the Task-008 Transactional Outbox inside the same PostgreSQL transaction as the aggregate change. The event carries identity, monotonic version, status and change kind, not the full product description.

## Security/rollback

All three tables use forced RLS. Application access has SELECT/INSERT/UPDATE policy shape and no DELETE policy. Hard DELETE/TRUNCATE are blocked by triggers. Aggregate identity is immutable and archived aggregates cannot be reactivated or edited.

The migration is additive and old binaries do not reference the new tables. Rollback therefore disables the catalog-aware binary while retaining catalog/outbox data; hard deletion is not a rollback mechanism.
