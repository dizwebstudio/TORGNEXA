# 1С-Битрикс

Storefront connector for self-hosted 1С-Битрикс («Управление сайтом»). The
connector uses the official REST catalog methods through a Bitrix REST module
webhook and is exposed in Settings → Integrations → Интернет-магазины.

The current runtime admits only product catalog read/write and product
inbound/outbound synchronization. Inventory, prices and orders remain SDK
capabilities without an executable application route until their contracts are
qualified end to end.

Official API references: [`catalog.product.list`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-list.html),
[`catalog.product.get`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-get.html),
[`catalog.product.add`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-add.html),
and [`catalog.product.update`](https://apidocs.bitrix24.ru/api-reference/catalog/product/catalog-product-update.html).
