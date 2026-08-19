# Magnit Market Capability Audit

Official sources audited 2026-08-10:
- https://seller-manual.mm.ru/magnit-market-seller-api
- https://github.com/magnit-tech/market-partner-api
- https://magnit-tech.github.io/market-partner-api/
- https://raw.githubusercontent.com/magnit-tech/market-partner-api/refs/heads/master/swagger.yaml

Audited official API: Magnit Market Partner API `v0.1.38`, OpenAPI `3.0.3`.
Production server: `https://b2b-api.magnit.ru`.
Authentication: `X-Api-Key`.

| Capability | Decision | Baseline |
|---|---|---|
| `products.read` | granted | `POST /api/seller/v1/products/sku/list`, scoped by configured `shop_id`; `sku_id` is the variant remote identity |
| `prices.read` | granted | shop-scoped SKU keyset via `POST /api/seller/v1/products/sku/shops/{shop_id}/short/list`, followed by `POST /api/seller/v1/products/sku/price/info` |
| `inventory.read` | granted with explicit aggregate boundary | `POST /api/seller/v1/products/sku/stocks/info`; current response exposes `stock_info_details.type`, `stock`, `reserved`, not a warehouse identifier |
| `orders.read` | granted | `POST /api/seller/v1/orders/list`; bounded rolling `created_at` window and opaque `next_page_token`; FBS assembly-task semantics |
| shop discovery | internal health/config validation only | `GET /api/seller/v1/shops` verifies the configured seller shop |
| `products.write` | denied | create/update/archive/delete SKU methods exist, but Task 035 grants no mutations |
| `prices.write` | denied | `/products/sku/price` exists, but write admission requires approval/compliance/idempotency review |
| `inventory.write` | denied | `/products/sku/stocks` exists, but write admission is deferred |
| `orders.status.write` | denied | cancel/complete/parcel/shipment methods exist, but are not admitted in Task 035 |
| warehouse-level stock read | unsupported in this baseline | current `/stocks/info` schema does not expose `warehouse_id`; connector returns an explicit per-shop/per-stock-type aggregate location rather than inventing warehouse allocation |
| inbound notifications | unsupported | no notification/webhook capability is granted from the audited Partner API surface |

## Identity boundary

- `product_id` and `sku_id` stay remote identifiers.
- TORGNEXA projects one bounded `RemoteProduct` per Magnit Market SKU because the official list surface is SKU-oriented and multiple SKU rows may share a product card.
- Product remote ID is the deterministic composite `<product_id>:<sku_id>`.
- Variant remote ID is the official `sku_id`.
- `seller_sku_id` and barcode remain aliases only.
- Order `order_id` remains the remote order identity; order items map by `sku_id`.

## Time/revision boundary

`/products/sku/list` has no card update timestamp in v0.1.38. Task 035 therefore joins the same page to `/products/sku/price/info` and uses that endpoint's official `timestamp` as the bounded observation timestamp. It does not synthesize `now()` as a remote revision. Task 013/014 payload fingerprints remain the authority for detecting title/alias/content drift.

## Inventory boundary

The read schema returns `stock_info_details` containing `stock`, `reserved`, and `type` (for example `FBS`). Available quantity is normalized as `stock - reserved`, fail-closed when either value is negative or `reserved > stock`. Only one detail for the configured stock type may exist. Missing configured type means explicit zero.

Task 035 does not claim a physical warehouse mapping because the audited read response does not contain `warehouse_id`. The synthetic location ID is deterministic and explicit: `shop:<shop_id>:stock-type:<type>`.

## Order/privacy boundary

The official order response includes `customer_id` and `delivery_region`; neither is copied into `RemoteOrder`. The connector only retains order identity/number, FBS status, timestamps and SKU quantities needed by sync/reconciliation.

`orders/list` requires a `created_at` range. On the first page the connector creates a configured rolling window (1-90 days); both endpoints of the window are bound into the opaque cursor together with the provider `page_token`, so subsequent pages use an immutable time range.

## Rate-limit boundary

The audited OpenAPI does not publish a normative request-rate ceiling. Task 035 therefore uses a conservative host policy (`max_concurrency=2`, minimum interval `500ms`, 15s timeout, bounded retry/backoff) and records live ceilings as staging/SLO evidence rather than inventing provider guarantees.
