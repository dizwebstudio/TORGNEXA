# Magento / Adobe Commerce

Коннектор для self-hosted Magento Open Source и Adobe Commerce. Авторизация —
долгоживущий bearer-токен активированной Integration в Admin. В runtime
маршрутизируются чтение товаров, цен, остатков, заказов и возвратов,
запись цен/остатков, обновление товара и отмена заказа. Возвраты остаются
отдельной read-поверхностью и не заявляются как generic worker-синхронизация.

Создание товара, переименование SKU и приём webhook намеренно не поддерживаются
(см. [capability-audit.md](capability-audit.md)). Для реальной проверки нужен
существующий синтетический SKU: [docker-live-qualification.md](docker-live-qualification.md)
и [scripts/magento-smoke.sh](../../../scripts/magento-smoke.sh) проверяют
credentialed REST API отдельно от SDK-конформанса.

Официальные материалы: [REST API](https://developer.adobe.com/commerce/webapi/rest/),
[Docker для Commerce](https://developer.adobe.com/commerce/contributor/guides/install/docker).
