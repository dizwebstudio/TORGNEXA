# Migration 000019 — search provider

Expand-only PostgreSQL search migration for canonical Products/Offers and Orders/OrderItems. It adds immutable `tsvector` helper functions plus GIN full-text and tenant-prefixed exact/prefix indexes; it does not add a second source of truth or an external search dependency.

Search reads continue to execute under the existing forced-RLS policies on `products`, `offers`, `orders`, and `order_items`, and repository SQL also binds organization/workspace predicates explicitly. Binary rollback leaves the additive functions/indexes in place and older binaries ignore them.

The GIN indexes are created transactionally because the repository migration runner currently uses embedded transactions. Production qualification must measure lock/build time on representative data before rollout; if table size makes this unsafe, use the existing Task-067 expand/backfill/contract machinery to introduce an online index-build deployment step rather than weakening tenant or search semantics.
