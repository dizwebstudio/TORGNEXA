# CS-Cart

Storefront-коннектор CS-Cart подключается через официальный REST API магазина
и показывается в разделе «Интеграции → Интернет-магазины».

В текущем runtime доступны операции каталога товаров: чтение и запись
(создание/обновление) с входящей и исходящей синхронизацией, а также чтение
базовых цен, остатков и заказов с входящей синхронизацией. Изменения
`commerce.catalog.product_changed.v1` обрабатывает worker-группа
`torgnexa.commerce-sync.v1`; для нового товара mapping создаётся после
подтверждённого ответа CS-Cart, а для существующего используется
tenant-scoped mapping и детерминированный idempotency key. Запись цен и
остатков выполняется через тот же product PUT с read-after-write и исходящей
синхронизацией; запись заказов и webhook-события пока не заявляются как
рабочие маршруты. Остаток доступен только как единый storefront balance.

Официальная документация: [REST API](https://docs.cs-cart.com/latest/developer_guide/api/index.html),
[Products](https://docs.cs-cart.com/latest/developer_guide/api/entities/products.html),
[Orders](https://docs.cs-cart.com/4.20.x/developer_guide/api/entities/orders.html).

Уровни проверки разделены: SDK-конформанс проходит 13/13 обязательных проб,
но live/Docker-квалификация требует лицензированный тестовый магазин и
включённый Basic Auth. Инструкция и smoke находятся в
[docker-live-qualification.md](docker-live-qualification.md); до их успешного
запуска live-проверка считается заблокированной.
