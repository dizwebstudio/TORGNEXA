# Magnit Market Connector Spec

## Provider

- ID: `magnit-market`
- Family: `marketplace`
- Version: `1.0.0`
- SDK major: `1`
- Production host: `b2b-api.magnit.ru`
- Auth: `X-Api-Key`, obtained only through Task 021 `SecretAccessor`

## Configuration

Host-owned account configuration:

- `shop_id`: positive active shop ID verified by `GET /api/seller/v1/shops`;
- `stock_type`: `FBS` or `FBO` aggregate selected explicitly;
- `order_window_days`: 1..90, default chosen by the host configuration layer.

Provider code receives no tenant selector and owns no persistent configuration storage.

## Supported read methods

### Products

1. Call `POST /api/seller/v1/products/sku/list` with `filter.shop_id` and bounded page pagination.
2. Validate unique positive `sku_id`, positive `product_id`, seller SKU/title and bounded barcode.
3. Batch the page's `sku_id` values through `/products/sku/price/info` to obtain the official timestamp.
4. Emit one `RemoteProduct` per SKU:
   - product remote ID `<product_id>:<sku_id>`;
   - variant remote ID `<sku_id>`;
   - seller SKU `seller_sku_id`;
   - barcode as a variant alias.

A full page yields a configuration-bound page cursor; an empty/short page closes pagination.

### Prices

1. Enumerate only the configured shop through `/products/sku/shops/{shop_id}/short/list` using official `last_key` keyset pagination.
2. Fetch exact price data for those `sku_id` values through `/products/sku/price/info`.
3. Preserve JSON number spelling with `json.Number`; exponent notation is rejected.
4. Emit `price`, optional `old_price`, currency and official `timestamp`.

### Inventory

1. Expose exactly one explicit aggregate location `shop:<shop_id>:stock-type:<FBS|FBO>`.
2. Query requested variant `sku_id` values through `/products/sku/stocks/info`.
3. Select only the configured stock type.
4. Normalize available quantity as `stock - reserved`.

The connector does not claim per-warehouse stock read because the current response schema does not expose warehouse identity.

### Orders

1. Use `POST /api/seller/v1/orders/list`.
2. First page captures a bounded rolling `created_at` window from the connector clock.
3. Opaque cursor stores provider `next_page_token` plus the immutable window and configuration fingerprint.
4. Emit FBS order ID/number/status/timestamps and SKU quantities only.
5. Never project buyer ID or delivery region.

## Failure model

- transport text is never surfaced;
- remote response bodies are never embedded in errors;
- 400/401/403/404/409/429/5xx normalize to bounded SDK categories;
- body > 12 MiB is rejected;
- malformed JSON, trailing JSON, duplicate IDs, unexpected SKU IDs and impossible stock arithmetic fail closed;
- cursor reuse after configuration change fails closed by SHA-256 fingerprint.

## Unsupported mutations

All official create/update/archive/delete, price update, stock update, cancel/complete, parcel and shipment operations are outside Task 035. Adding any write capability requires a separate architecture review with Tasks 017/028/082, dry-run/idempotency and audit/lineage evidence.
