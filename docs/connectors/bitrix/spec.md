# 1С-Битрикс Connector Spec

Family: `storefront`. Deployment: self-hosted 1С-Битрикс with the REST
module enabled and a webhook that is allowed to call the catalog methods.
The Bitrix24 REST documentation is the canonical method/shape reference; a
self-hosted installation must expose the same REST catalog surface before a
tenant enables the connector.

Authentication is a webhook credential stored as encrypted SecretProvider
material, not in runtime configuration. The JSON shape is:

```json
{"user_id":"1","webhook_code":"replace-with-webhook-code"}
```

Non-secret runtime configuration:

```json
{
  "store_host": "shop.example.ru",
  "base_path": "",
  "catalog_iblock_id": 23,
  "store_currency": "RUB"
}
```

`catalog.product.list` is paged with `start` offsets and is filtered by the
configured `iblockId`. The connector reads `id`, `name`, `active`, `xmlId`,
`code`, `detailText` and timestamps and maps one Bitrix product to one
TORGNEXA product variant. Product writes use `catalog.product.update` when a
remote ID is present; creates use `catalog.product.add` after an exact `xmlId`
lookup, read the returned `result.element.id`, then re-fetch the product to
verify the result.

All network access is host-mediated by the reviewed builtin runtime transport.
The connector package has no direct HTTP, database or Core imports. Remote
errors, malformed envelopes and ambiguous write outcomes are normalized; an
ambiguous write is reconciled by a read rather than blindly retried.

Official references: [`catalog.product.list`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-list.html),
[`catalog.product.get`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-get.html),
[`catalog.product.add`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-add.html),
and [`catalog.product.update`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-update.html).
