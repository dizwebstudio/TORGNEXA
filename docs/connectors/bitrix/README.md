# 1С-Битрикс

Storefront connector for self-hosted 1С-Битрикс («Управление сайтом»). The
connector uses the official REST catalog methods through a Bitrix REST module
webhook and is exposed in Settings → Integrations → Интернет-магазины.

The current runtime admits product, price, inventory and order reads, plus
product/price/inventory writes and order-status writes. Product, price,
inventory and order events are routed through the tenant-scoped commerce-sync
worker; inventory writes require an explicit warehouse mapping and order
status writes require the configured canonical status map. Live qualification
still remains separate from repository/runtime admission.

Official API references: [`catalog.product.list`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-list.html),
[`catalog.product.get`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-get.html),
[`catalog.product.add`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-add.html),
and [`catalog.product.update`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-update.html).
