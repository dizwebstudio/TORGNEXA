# Task 096 — OpenCart Connector Contract v1

OpenCart is a storefront provider behind Connector SDK v1. TORGNEXA speaks only to a versioned shop-local extension API and never to the shop database directly.

## Bridge endpoint
All calls use the configured HTTPS store `index.php` endpoint with route `extension/torgnexa/api/<operation>`. The bridge bearer token is callback-scoped. `health` MUST return `{"ok":true,"api_version":"v1"}`.

## Required v1 operations
GET: `health`, `products`, `product`, `product-by-sku`, `variant`, `orders`, `order`.
POST: `product`.
PUT: `product`, `variant-price`, `variant-inventory`, `order-status`.

Product and order list operations return bounded `page`/`total_pages` envelopes. Timestamps are RFC3339 UTC-capable values. The bridge owns OpenCart-internal model/version differences and emits stable TORGNEXA-facing JSON.

## Write safety
Product create/update uses a stable SKU and idempotency key. Exact price, inventory and order status mutations require read-before/read-after verification. An ambiguous write is never blindly repeated; it is reconciled and otherwise fails with `write_outcome_unknown`.
