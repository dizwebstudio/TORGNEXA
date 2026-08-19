# OpenCart bridge provider spec v1

Transport endpoint: configured HTTPS store `/index.php` with query route `extension/torgnexa/api/<operation>`.

Authentication: bearer token provisioned in the shop extension and stored only through TORGNEXA SecretAccessor.

Health response: `{ "ok": true, "api_version": "v1" }`.

List response: `{ "items": [...], "page": 1, "total_pages": 1 }`.

The bridge emits stable product/variant/order JSON independent of OpenCart internal schema differences. Product write accepts `id`, `sku`, `title`, `description`, `status`, `idempotency_key`. Variant price/inventory and order status are exact-state PUT operations.
