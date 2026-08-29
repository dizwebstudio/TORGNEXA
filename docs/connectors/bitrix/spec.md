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
  "store_currency": "RUB",
  "price_type_id": 1,
  "order_statuses": {
    "pending": "<bitrix-status-id>",
    "confirmed": "<bitrix-status-id>",
    "processing": "<bitrix-status-id>",
    "fulfilled": "<bitrix-status-id>",
    "cancelled": "<bitrix-status-id>"
  }
}
```

`catalog.product.list` is paged with `start` offsets and is filtered by the
configured `iblockId`. The connector reads `id`, `name`, `active`, `xmlId`,
`code`, `detailText` and timestamps and maps one Bitrix product to one
TORGNEXA product variant. Product writes use `catalog.product.update` when a
remote ID is present; creates use `catalog.product.add` after an exact `xmlId`
lookup, read the returned `result.element.id`, then re-fetch the product to
verify the result.

Outbound regular prices use the configured `price_type_id`. The connector
first looks up the product's existing price of that type, then calls
`catalog.price.update` or `catalog.price.add` and reads the price back. It
never uses `catalog.price.modify`, because that method removes prices omitted
from the submitted collection. The generic worker routes canonical price
events to this operation through an enabled outbound `prices` policy.

Inventory reads are available in the connector SDK: active warehouses are
resolved with `catalog.store.list`, and a bounded set of product balances is
read with `catalog.storeproduct.list`. Bitrix returns `amount` as a number,
while the SDK v1 inventory port is integer-based; fractional values are
rejected instead of rounded. Missing product rows are reported as zero for
the requested warehouse. The production reconciliation runtime does not yet
admit this read surface because its generic inventory source bridge is a
separate implementation step.

Inventory writes use a mapped remote warehouse. Because Bitrix's catalog
stock amount is a read-only projection, an absolute integer quantity is
translated into a positive-delta `S` (оприходование) or negative-delta `D`
(списание) warehouse document. The document number is derived from the
idempotency key; retries resume an existing draft, add one document element,
conduct the document and verify the resulting balance. The worker requires a
tenant-scoped `warehouse` mapping before routing an inventory event, so a
canonical warehouse can never be silently sent to another remote warehouse.
Fractional quantities remain fail-closed in SDK v1.

Order reads use `sale.order.list` and enrich each page with
`sale.basketitem.list`. Bitrix custom basket lines with `productId=0` are
excluded from the canonical catalog-line projection; fractional quantities
are rejected because SDK v1 order quantities are integers. Order status writes
use `sale.order.update` and verify with `sale.order.get`. The runtime requires
all five canonical lifecycle values (`pending`, `confirmed`, `processing`,
`fulfilled`, `cancelled`) to be mapped explicitly to the installation's
Bitrix status IDs. The worker then exposes inbound order reconciliation and
outbound canonical status events; unknown or unmapped statuses remain
fail-closed.

All network access is host-mediated by the reviewed builtin runtime transport.
The connector package has no direct HTTP, database or Core imports. Remote
errors, malformed envelopes and ambiguous write outcomes are normalized; an
ambiguous write is reconciled by a read rather than blindly retried.

Official references: [`catalog.product.list`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-list.html),
[`catalog.product.get`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-get.html),
[`catalog.product.add`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-add.html),
 [`catalog.product.update`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-update.html),
 [`catalog.price.list`](https://apidocs.bitrix24.ru/api-reference/catalog/price/catalog-price-list.html),
 [`catalog.price.add`](https://apidocs.bitrix24.ru/api-reference/catalog/price/catalog-price-add.html),
 [`catalog.price.update`](https://apidocs.bitrix24.ru/api-reference/catalog/price/catalog-price-update.html),
 [`catalog.store.list`](https://apidocs.bitrix24.ru/api-reference/catalog/store/catalog-store-list.html),
 [`catalog.storeproduct.list`](https://apidocs.bitrix24.ru/api-reference/catalog/store-product/catalog-store-product-list.html),
 [`sale.order.list`](https://apidocs.bitrix24.ru/api-reference/sale/order/sale-order-list.html),
 [`sale.basketitem.list`](https://apidocs.bitrix24.ru/api-reference/sale/basket-item/sale-basket-item-list.html),
 [`sale.order.get`](https://apidocs.bitrix24.ru/api-reference/sale/order/sale-order-get.html),
 and [`sale.order.update`](https://apidocs.bitrix24.ru/api-reference/sale/order/sale-order-update.html).
