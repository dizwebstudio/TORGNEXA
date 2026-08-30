# Medusa v2

Коннектор для self-hosted Medusa v2. Секретный API key передаётся как raw
token в `Authorization: Basic <token>`; это не пара логин/пароль и не bearer.
В runtime маршрутизируются чтение товаров, цен, остатков, заказов и возвратов,
запись цен/остатков, обновление товара и отмена заказа. Возвраты остаются
отдельной read-поверхностью и не заявляются как generic worker-синхронизация.

Создание товара и входящие webhook намеренно не поддерживаются (см.
[capability-audit.md](capability-audit.md)). Для проверки нужен заранее
созданный синтетический SKU. Изолированный Docker-стенд и пошаговая credentialed
проверка описаны в [docker-live-qualification.md](docker-live-qualification.md);
[scripts/medusa-smoke.sh](../../../scripts/medusa-smoke.sh) проверяет конкретный
Admin REST endpoint отдельно от SDK-конформанса. На 2026-08-29 Docker smoke
для DTC Starter прошёл с read-after-write по товару, цене и остатку; внешний
staging endpoint ещё требует собственные credentials.

Официальные материалы: [Medusa v2 Admin API](https://docs.medusajs.com/api/admin),
[установка через Docker](https://docs.medusajs.com/learn/installation/docker).
