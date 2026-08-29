# Shopify

Storefront connector. OAuth2 per-tenant host, Shopify Admin REST API for
products/inventory/prices/orders/returns. Product create and webhook receipt
are unsupported by design (see capability-audit.md).

The connector is pinned to Admin REST API `2026-07`, the current stable version
used for qualification. Shopify is SaaS and has no official self-hosted Docker
store, so local verification uses a protocol double; real qualification needs a
Shopify Dev Store and app token.

Docker/protocol and Dev Store instructions are in
[docker-live-qualification.md](docker-live-qualification.md). The executable
[scripts/shopify-smoke.sh](../../../scripts/shopify-smoke.sh) checks access,
catalog, inventory, prices, orders/refunds, safe writes and cleanup. The latest
protocol result is recorded in
[live-qualification-status.json](live-qualification-status.json).

Official documentation: [Admin REST API](https://shopify.dev/docs/api/admin-rest/latest),
[Dev stores](https://shopify.dev/docs/apps/build/stores/development-stores),
[API versioning](https://shopify.dev/docs/api/usage/versioning)
