# ADR 0043: Canonical provider-neutral PIM / MDM masters and reviewable merge semantics

Status: Accepted

## Context

Task 004 established canonical Product and Offer identity, while connector mappings kept remote product/offer/order identifiers outside Core. The platform still lacked canonical Brand, Category and Attribute masters, field-level source authority, reviewable duplicate evidence and a deterministic preview of how competing master-data sources would merge. Implementing marketplace taxonomies directly on Product/Offer would create provider branches in Core and make later synchronization/reconciliation non-deterministic.

## Decision

Task 023 adds `internal/core/pim` for provider-neutral Brand, Category, Attribute, Product master-data relationships, field-authority, duplicate-candidate and merge-preview primitives. PostgreSQL persistence lives in `internal/platform/postgres/pimrepo` and additive migration `000015_pim_mdm.sql`.

Brand, Category and Attribute have stable local identity, immutable codes/type identity, forward-only lifecycle and optimistic versions. Category parent is immutable in PIM v1; because a parent must already exist and cannot later change, cycles cannot be introduced by re-parenting. Product master data is attached through tenant-scoped Brand/Category/Attribute relationship tables rather than provider-specific Product columns.

External Brand/Category/Attribute identity reuses `connector_entity_mappings`. Published mapping v1/v2 contracts remain unchanged; additive v3 broadens the canonical entity-type set.

Field authority is explicit per canonical entity type, field path and source. Merge preview is deterministic and non-executing: missing fields can be filled, higher-authority sources win, equal values are recognized, and differing equal-authority values remain explicit conflicts. Duplicate candidates retain bounded score and human-readable signals so review is explainable rather than a black-box boolean.

PIM mutations commit Task-003 audit evidence, Task-008 outbox intent and Task-030 lineage references in the same PostgreSQL transaction.

## Consequences

Marketplace/ERP taxonomies can project onto a stable canonical master without contaminating Product/Offer. Import, synchronization and reconciliation can compute a preview before mutating canonical data. Duplicate handling becomes reviewable and field ownership becomes auditable. Later Legal Entity, Product Compliance, procurement, WMS and connectors can reference canonical master IDs.

## Alternatives considered

Adding `ozon_category_id`, `wb_category_id` or equivalent provider fields to Product was rejected because it violates the provider-neutral Core boundary. Storing all PIM values in one unconstrained JSON document was rejected because it weakens type safety, indexing and field authority. Automatically applying duplicate merges was rejected because equal-authority conflicts require explicit review and sensitive merges may require Task-017 approval. Using floating-point JSON numbers for decimal attributes was rejected because Task 076 requires exact decimal semantics.

## Compatibility impact

The change is additive. Existing Product/Offer and connector-mapping v1/v2 contracts remain valid. New PIM contracts and mapping v3 are introduced, and one new versioned PIM event is registered. No existing event payload is modified.

## Migration and data impact

Expand migration `000015_pim_mdm.sql` creates Brand/Category/Attribute masters, Product master-data relationships, field-authority rules, duplicate candidates and merge previews. It broadens the existing connector mapping CHECK/guard to three additional canonical entity types. No existing table/column is dropped or renamed. Historical evidence is retained on binary rollback.

## Security and privacy impact

All PIM tables are tenant-scoped with composite foreign keys and forced RLS. Hard delete/truncate is blocked for master/history records. Duplicate and merge artifacts store bounded master-data evidence, not credentials or customer PII. Provider-specific identifiers remain in connector mapping storage and do not enter Core structures.

## Operational impact

Support/reconciliation can explain why a source won a field and why two records were flagged as duplicates. Merge previews are fingerprinted and retry-safe. Backup/restore and migration rehearsals must include the new tables. Future import/reconciliation flows should surface preview conflicts rather than silently overwriting canonical data.
