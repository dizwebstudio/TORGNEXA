# CS-Cart

Storefront-коннектор CS-Cart подключается через официальный REST API магазина
и показывается в разделе «Интеграции → Интернет-магазины».

В текущем runtime доступны только операции каталога товаров: чтение и запись
(создание/обновление) с входящей и исходящей синхронизацией. Заказы, цены,
остатки и webhook-события пока не заявляются как рабочие маршруты.

Официальная документация: [REST API](https://docs.cs-cart.com/latest/developer_guide/api/index.html),
[Products](https://docs.cs-cart.com/latest/developer_guide/api/entities/products.html).
