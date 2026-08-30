# PrestaShop connector

Task 095 adds a native PrestaShop Webservice provider to TORGNEXA.

Supported: products/combinations read, prices read/write, StockAvailable inventory read/write, orders read and order-state transition writes. Multi-language context is mandatory; optional multi-shop context is supported.

В production runtime подключены маршруты для `prices`, `inventory` и `orders`:
изменения из PostgreSQL/outbox попадают в Kafka, обрабатываются worker-группой
`torgnexa.commerce-sync.v1` и отправляются в PrestaShop только при включённой
политике, capability и существующем `offer` mapping. Для заказов чтение
использует `orders` + `order_details`, а смена состояния —
`order_histories`; карта `order_statuses` хранится в tenant-scoped runtime
configuration. Повторная доставка использует детерминированный idempotency
key и `sync_local_receipts`; временные ошибки получают Kafka retry, а ошибки
конфигурации, схемы или отсутствующее сопоставление — DLQ.

The connector intentionally does not project customer addresses, emails or names.

Локальная проверка настоящего Webservice API описана в
[docker-smoke.md](docker-smoke.md): стенд поднимает официальный PrestaShop
8.1 + MariaDB, создаёт синтетические товары и проверяет Basic Auth, JSON-
чтения и XML `PATCH` для цены и `StockAvailable`.

В интерфейсе та же инструкция доступна по адресу `/docs#prestashop-smoke` и
содержит снимки storefront и API-проверок. Smoke проверяет Webservice API;
end-to-end worker-маршрут дополнительно требует поднятый TORGNEXA Compose,
активный connector account, включённые `prices.write`/`inventory.write`/
`orders.status.write`, пять `order_statuses` и `offer` mapping.
