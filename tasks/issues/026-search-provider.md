# Task 026: Search Provider

## Status
Repository-complete. Live PostgreSQL query-plan/index-build qualification remains deployment evidence, not missing repository implementation.

## Objective
Implement a provider-neutral SearchProvider with PostgreSQL-backed Product/Order search and tenant authorization tests.

## Dependencies
028

## Deliverables
Provider contract, PostgreSQL FTS/prefix adapter and migration, authenticated REST search/list surfaces, cursor/result schemas, tenant/RLS tests, architecture evidence and documentation.

## Acceptance
Product search covers Product code/title/description plus Offer SKU/GTIN; Order search covers order number plus OrderItem SKU snapshot. Search is tenant/workspace scoped from authenticated context, explicit SQL predicates and forced RLS; client tenant identifiers cannot override scope. Pagination is bounded and cursor-based; query/cursor misuse fails closed. PostgreSQL remains truth and no external search dependency is introduced. Required repository checks pass.
