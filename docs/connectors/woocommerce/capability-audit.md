# WooCommerce capability audit

Qualified official surface: WP REST API v3 under `/wp-json/wc/v3/`. Consumer Key / Consumer Secret authenticate over HTTPS using HTTP Basic Authentication. Query-string credentials are deliberately forbidden because they can leak through URL/access logs.

Admitted capabilities:

- `products.read`, `products.write` — simple-product create/update plus simple/variable reads;
- `prices.read`, `prices.write` — simple product and variation prices;
- `inventory.read`, `inventory.write` — managed stock only; unknown/unmanaged stock is not converted into fabricated quantities;
- `orders.read`, `orders.status.write`;
- `returns.read` — WooCommerce order refunds are normalized as returns;
- `notifications.receive` — HMAC-SHA256 webhook verification plus host-owned durable replay deduplication.

Product create requires a stable seller SKU. Before create, and after an ambiguous network/5xx outcome, the adapter reconciles by SKU instead of blindly repeating POST. Existing product/price/inventory/status writes are exact-state operations and perform read-after-write reconciliation on ambiguous outcomes. If an effect cannot be proven the adapter fails closed with `write_outcome_unknown`.

Variable-product creation is intentionally not admitted in v1 because WooCommerce variation creation requires store-specific attribute/taxonomy mapping. Coupons/customers/order creation/webhook provisioning are also deferred until provider-neutral SDK semantics are qualified rather than exposed as provider-only methods.

Official sources:
- https://developer.woocommerce.com/docs/apis/rest-api/v3/
- https://developer.woocommerce.com/docs/apis/rest-api/v3/products/
- https://developer.woocommerce.com/docs/apis/rest-api/v3/product-variations/
- https://developer.woocommerce.com/docs/apis/rest-api/v3/orders/
- https://developer.woocommerce.com/docs/apis/rest-api/v3/order-refunds/
- https://developer.woocommerce.com/docs/apis/rest-api/v3/webhooks/

Privacy boundary: raw webhook JSON is used only for HMAC verification/resource-envelope parsing. The connector does not project billing/shipping/customer fields from webhook bodies.
