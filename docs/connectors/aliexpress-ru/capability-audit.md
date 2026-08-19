# AliExpress RU Capability Audit

Audited 2026-08-10 against the Russia-facing seller documentation, not the global AliExpress Open Platform.

Primary seller sources:
- https://business.aliexpress.ru/docs/api-token
- https://business.aliexpress.ru/docs/product-list
- https://business.aliexpress.ru/docs/manage-stocks
- https://business.aliexpress.ru/docs/local-get-orders-list
- https://business.aliexpress.ru/docs/changelog
- https://help.aliexpress-cis.com/help/article/api-token

Russia seller API host: `https://openapi.aliexpress.ru`.
Authentication: JWT placed in the `X-Auth-Token` header.

The official change history is part of the admission decision: legacy product-list stock fields (`ipm_sku_stock` / `stock`) were deprecated, a separate stock-read API was introduced later, and local order-list APIs evolved independently of global AliExpress APIs. Task 036 therefore does not infer current inventory/order semantics from the global platform or deprecated fields.

| Capability | Decision | Evidence / boundary |
|---|---|---|
| `products.read` | **granted** | Russia-specific filtered product scroll is current seller functionality; repository baseline uses `POST /api/v1/scroll-short-product-by-filter`, `last_product_id`, bounded `limit`, product `id`, `ali_updated_at`, `subject`, and SKU identifiers/codes. |
| `inventory.read` | **deferred** | legacy stock fields embedded in product-list DTOs are explicitly deprecated. Official change history confirms a separate stock-read API exists, but Task 036 does not admit it until its current Russia-facing request/response contract is captured with account-backed evidence. |
| `prices.read` | **deferred** | product DTOs contain price-shaped fields, but Task 036 does not promote them into an independent price capability without a current authoritative read contract/freshness semantic. |
| `orders.read` | **deferred** | official Russia seller documentation/change history confirms local order-list functionality, but exact current response-schema qualification is kept for a staged capability extension instead of importing assumptions from global order APIs. |
| all product/price/stock/order writes | **denied** | mutation endpoints are outside the baseline; any admission requires approval/idempotency/audit review. |
| inbound notifications | **unsupported** | no Russia-facing notification contract is admitted by Task 036. |

## Identity boundary

- AliExpress product `id` remains a remote Product mapping key.
- SKU `sku_id` is preferred as the remote Variant mapping key; the API's internal SKU `id` is only a fallback when `sku_id` is absent.
- seller SKU `code` remains an alias/SKU string.
- none of these identifiers are added to Core schemas.

## Revision boundary

`ali_updated_at` is the only admitted product observation timestamp. Task 036 never substitutes local `now()` as a remote revision. Payload fingerprints from Tasks 013/014 remain the drift authority for content changes.

## Pagination boundary

The product cursor stores only the last product ID plus a connector-surface fingerprint. It is opaque to the host, bounded, versioned, rejects unknown/trailing JSON, and cannot be reused against a different connector surface/version.

## Deprecated stock boundary

`ipm_sku_stock` may still be present in legacy/generated DTOs. The parser deliberately ignores it. Its presence does **not** authorize `inventory.read` and cannot feed canonical inventory.

## Rate-limit boundary

Russia seller token documentation publishes an upper ceiling far above the connector's policy. Task 036 intentionally configures a conservative host-owned policy (`max_concurrency=4`, `100ms` minimum interval, 15s timeout, bounded retries). Live account-specific throttling remains staging/SLO evidence.

## Evidence quality / remaining qualification

Repository fixtures pin only the admitted product-read shape. A live Russia seller account must qualify current stock, order and price read contracts before those capabilities can be added. This is intentional fail-closed capability admission, not an implementation gap hidden behind global AliExpress documentation.
