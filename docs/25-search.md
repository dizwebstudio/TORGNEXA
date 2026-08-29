# Search Platform

TORGNEXA depends on the provider-neutral `internal/platform/search.Provider` contract. PostgreSQL is the Task-026 MVP backend; adding OpenSearch/Elasticsearch is not required and must be justified by measured scale or feature requirements rather than introduced as a second operational truth.

## Task 026 scope

The MVP supports authenticated tenant/workspace search for:

- Products by canonical product code/title/description and related Offer SKU/GTIN;
- Orders by canonical order number and immutable OrderItem SKU snapshot;
- optional canonical status filters;
- optional UTC `placed_from` / `placed_to` range for orders;
- bounded pages (`1..100`) with opaque keyset cursors.

An empty `q` is a tenant-scoped list operation. Product results include the canonical description and an optional representative regular price (`minor_units` + `currency`) from the first active offer, so catalog tables can render useful merchandising context without loading every offer. Results still omit organization/workspace identifiers, order items, customer content and credentials.

## Authorization and tenancy

The HTTP surface obtains `tenancy.Scope` only from an authenticated `SearchScopeResolver`. `organization_id`, `workspace_id` or similar client query values are not inputs to the provider contract and cannot override authenticated scope.

The PostgreSQL adapter applies both defenses on every request:

1. transaction-local `app.organization_id` / `app.workspace_id`, activating the existing forced-RLS policies on `products`, `offers`, `orders`, and `order_items`;
2. explicit organization/workspace predicates on root rows and every child `EXISTS` search path.

Failure to obtain a valid authenticated scope is fail-closed and the provider is not called.

## PostgreSQL search implementation

Migration `000019_search_provider.sql` adds immutable `tsvector` helper functions and GIN indexes over canonical source fields plus tenant-prefixed lower-case prefix indexes. Search remains a derived read capability over the PostgreSQL system of record; there is no independently mutable search table.

Ranking is deterministic and deliberately simple for the MVP:

1. exact canonical code/order-number/SKU/GTIN match;
2. identifier/title prefix match;
3. remaining PostgreSQL FTS match.

Rows inside a rank are ordered by `updated_at DESC, id DESC`. The next cursor contains that keyset position plus a SHA-256 fingerprint of the original normalized search/filter set. A cursor from a different query is rejected.

## API

`GET|HEAD /api/v1/products` and `GET|HEAD /api/v1/orders` implement the existing list surfaces as search-capable reads. Responses use `Cache-Control: no-store` and the versioned schemas in `contracts/search/`.

## Operational notes

GIN index creation is additive but can be expensive on large tables. The repository migration uses the existing embedded-transaction runner; production qualification must measure build/lock time on representative data. If that is unsafe at deployed scale, introduce an online index-build deployment stage through the Task-067 migration framework rather than weakening tenant isolation or running an untracked manual schema change.

Task 061 retention/deletion must continue to treat any future external search backend as derived data and include deletion propagation. No external backend is introduced by Task 026.
