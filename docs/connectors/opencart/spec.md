# OpenCart bridge provider spec v1

Transport endpoint: configured HTTPS store `/index.php` with query route `extension/torgnexa/api/<operation>`.

Authentication: bearer token provisioned in the shop extension and stored only through TORGNEXA SecretAccessor.

Health response: `{ "ok": true, "api_version": "v1" }`.

List response: `{ "items": [...], "page": 1, "total_pages": 1 }`.

The bridge emits stable product/variant/order JSON independent of OpenCart internal schema differences. Product write accepts `id`, `sku`, `title`, `description`, `status`, `idempotency_key`. Variant price/inventory and order status are exact-state PUT operations.

The reference extension is an installable OpenCart 4.x payload under
`connectors/storefronts/opencart/extension/torgnexa`. Its product projection
maps a simple OpenCart product to the connector's single variant
`product:<product_id>`. Optional compare-at prices are kept in the extension's
`torgnexa_variant_meta` table because OpenCart's base product schema has no
portable compare-at field. SKU reconciliation uses OpenCart 4's native
`product_code` table (`code = 'SKU', value = <sku>`) rather than assuming a
`product.sku` column. Order projections include only IDs, statuses, timestamps
and line quantities. Since OpenCart has no private/archived product state, the
bridge stores provider-neutral `private`/`archived` writes as unpublished
(`draft`) and the connector reconciles those equivalent states.
