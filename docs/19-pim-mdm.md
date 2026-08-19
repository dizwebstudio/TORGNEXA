# PIM / MDM

Canonical master-data layer for products, brands, categories, attributes, variants, GTIN and deduplication.

Rules:
- marketplace cards are projections, not masters;
- every external category/attribute/brand identity maps to canonical entities with versioned generic connector mappings;
- duplicate detection is explainable and reviewable;
- imports and reconciliation can build a deterministic merge preview before any merge is applied;
- authoritative field ownership is configurable per canonical entity type, field and source;
- Product/Offer never gain provider-specific category or attribute columns.

## Implemented baseline — Task 023

Task 004 owns canonical Product/Offer. Task 023 adds:
- canonical `Brand`, hierarchical `Category`, and typed `Attribute` definitions;
- Product↔Brand, Product↔Category and typed Product Attribute Value records;
- exact decimal attribute encoding as JSON strings (no floating-point master-data drift);
- `FieldAuthority` rules with explicit source priority;
- explainable duplicate candidates with bounded signal evidence and review states;
- deterministic immutable merge previews. Equal-authority differing values remain explicit conflicts rather than silently choosing a winner;
- generic connector mapping v3 for Product/Offer/Order/Brand/Category/Attribute;
- atomic Audit + Outbox + Lineage evidence for PIM mutations.

Merge preview is deliberately not merge execution. A caller must resolve preview conflicts and any sensitive write remains subject to Task 017 approval. Task 030 lineage records the exact source/version inputs and transformation evidence.
