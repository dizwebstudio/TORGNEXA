# Magento / Adobe Commerce: Docker и credentialed smoke

## Почему в репозитории нет готового образа

Magento Open Source и Adobe Commerce требуют полноценного проекта, Composer
пакетов и настроек базы/поиска. Adobe описывает Docker как способ поднять
собственный или существующий проект, а не как публичный универсальный demo-
образ. Для Adobe Commerce пакет также требует доступ к Composer repository и
учётные данные проекта. Поэтому случайный сторонний образ не был бы проверкой
официального продукта и мог бы скрыть несовместимости.

Используйте официальные инструкции:

- [Docker install guide](https://developer.adobe.com/commerce/contributor/guides/install/docker);
- [Adobe Commerce Cloud Docker](https://developer.adobe.com/commerce/cloud-tools/docker/);
- [Initialize Docker for an on-premises project](https://developer.adobe.com/commerce/cloud-tools/docker/setup/initialize-docker).

Ожидаемый порядок для отдельного non-production проекта:

1. Получите Magento Open Source или лицензированный Adobe Commerce package и
   Composer credentials. Не подключайте production database или ключи.
2. В каталоге проекта установите `magento/magento-cloud-docker` по официальной
   инструкции и сгенерируйте Compose-конфигурацию командой `ece-docker
   build:compose`. Точный PHP/Elasticsearch/OpenSearch профиль берите из
   версии проекта; не смешивайте несовместимые версии.
3. Запустите сгенерированный Compose-проект, завершите установку Magento и
   создайте отдельную Integration с ACL только для smoke-проверки.
4. Создайте в Admin один синтетический простой товар с SKU
   `TORGNEXA-MAGENTO-SMOKE` (коннектор намеренно не создаёт товар: для этого
   нужны `attribute_set_id`, `type_id` и другие поля, которых нет в общем
   `ProductWriteRequest`). При необходимости подготовьте отдельный
   синтетический order id для read-проверок.

## Credentialed smoke

Скрипт запускается из корня TORGNEXA и не записывает секреты в репозиторий:

```bash
export MAGENTO_BASE_URL=https://magento-staging.example.test
export MAGENTO_TOKEN='token-from-an-activated-integration'
export MAGENTO_TEST_SKU=TORGNEXA-MAGENTO-SMOKE
scripts/magento-smoke.sh
```

Для локального HTTP-only контейнера допустимо только loopback:

```bash
MAGENTO_BASE_URL=http://127.0.0.1:8080 \
MAGENTO_TOKEN='local-integration-token' \
MAGENTO_TEST_SKU=TORGNEXA-MAGENTO-SMOKE \
MAGENTO_ALLOW_HTTP=1 \
scripts/magento-smoke.sh
```

Самоподписанный TLS разрешается только явно через `MAGENTO_INSECURE_TLS=1`.
Для удалённого стенда сертификат должен проверяться штатно.

По умолчанию smoke выполняет read-only проверки:

- `401`/`403` без bearer и успешный `GET /rest/V1/products` с токеном;
- bounded catalog read и `GET /products/{sku}`;
- legacy CatalogInventory `GET /stockItems/{sku}`;
- при заданном `MAGENTO_TEST_ORDER_ID` — чтение заказа и creditmemos.

Запись запускается только явно на disposable/non-production товаре:

```bash
MAGENTO_ALLOW_WRITES=1 scripts/magento-smoke.sh
```

В этом режиме проверяются обновление названия/описания, цены и остатка с
read-after-write reconciliation. Исходные значения автоматически
восстанавливаются в `trap`; `MAGENTO_KEEP_CHANGES=1` оставляет их для ручного
осмотра. Отмену заказа smoke не выполняет: `POST /orders/{id}/cancel` является
необратимой операцией и проверяется отдельным согласованным disposable-order
тестом.

Успешная последняя строка:

```text
Magento / Adobe Commerce REST smoke: all checks passed
```

Это подтверждает конкретную установку и ACL, но не превращает операции,
которые production worker ещё не маршрутизирует, в заявленную generic-
синхронизацию. До запуска на реальном стенде статус остаётся `BLOCKED` в
`live-qualification-status.json`.
