# ADR 0086: WooCommerce storefront as a bidirectional commerce provider

## Status
Accepted for Task 094.

## Context
TORGNEXA already integrates marketplaces, classified channels and social networks, but a merchant-owned storefront is a distinct commerce source/sink. WooCommerce exposes an official current WP REST API v3 for products, variations, orders, refunds and webhooks. The frozen Connector SDK v1 contained commerce write capability names but lacked typed additive write contracts.

## Decision
Admit WooCommerce as marketplace-family provider `woocommerce` using only `/wp-json/wc/v3/` over host-enforced HTTPS. Add provider-neutral, additive SDK-v1 commerce write/return/webhook interfaces without changing the root `Connector` or `Runtime` interfaces. Consumer Key/Secret remain callback-scoped Task-021 material and query-string authentication is forbidden.

Products and orders are read through bounded cursor scans; variable-product variations are normalized into mapping-only remote IDs. Managed stock is explicit and unmanaged stock is never fabricated. Product create is reconciled by unique seller SKU after ambiguous outcomes. Exact price, inventory and order-status writes use read-before/read-after reconciliation. Webhooks verify body HMAC-SHA256 and use host-known expected topics plus durable replay claims.

## Alternatives considered
Provider-specific untyped write methods were rejected because they would undermine SDK portability. Legacy WooCommerce REST endpoints were rejected because v3 is the recommended current integration. Query-string Consumer credentials were rejected due URL/log leakage. Blind retry of POST create was rejected because WooCommerce does not expose a generic idempotency-key header. Fabricating stock for unmanaged products was rejected because `instock` is not a quantity.

## Compatibility impact
Root Connector SDK interfaces are unchanged. New interfaces are additive capability surfaces. Existing providers need no changes. The notification resource vocabulary is additively widened for product/coupon/customer webhook kinds.

## Migration and data impact
No PostgreSQL schema migration. Remote identities continue through Task-010 mappings; webhook dedup/evidence uses existing host inbox primitives.

## Security and privacy impact
Secrets stay callback-scoped. Store configuration accepts hostname/base-path rather than arbitrary URLs, reducing SSRF surface. Webhook signatures are constant-time verified. Delivery headers are not treated as authenticated replay identity. Orders are projected without billing/shipping/customer PII in the Connector SDK read model.

## Operational impact
A WooCommerce account requires a read/write REST key, a dedicated webhook secret and a fixed store host/currency. Store plugin/custom-field semantics remain outside the base connector until explicitly mapped.

## Consequences
TORGNEXA gains its first merchant-owned storefront provider and a reusable typed commerce-write SDK boundary. Shopify/OpenCart/PrestaShop/Adobe Commerce can reuse the same provider-neutral surfaces rather than adding Core branches.
