# Shopware 6: Docker и credentialed Admin API smoke

## Что проверяем

Shopware 6 предоставляет Admin API под `/api/*`: интеграции получают короткий
OAuth2 bearer-токен через `client_credentials`, а поиск сущностей выполняется
через `POST /api/search/{entity}`. Текущая проверка использует синтетический
Shopware 6.7.2.2 с demo-каталогом и временной Integration credential.

Shopware рекомендует Docker для локальной разработки. В этом репозитории
используется disposable all-in-one образ `dockware/shopware:6.7.2.2`: он удобен
для smoke на небольшой VPS, но поддерживается сообществом Dockware, а не
командой Shopware. Это не production-образ и не внешний merchant staging.

## Изолированный стенд

Из корня TORGNEXA:

```bash
docker compose -f docker-compose.shopware-test.yml up -d
docker compose -f docker-compose.shopware-test.yml ps
docker compose -f docker-compose.shopware-test.yml logs -f shopware
```

Готовность определяется по строке `container IS READY` в логах. API доступен
только на `http://127.0.0.1:18005`; заголовок `Host: localhost` нужен Dockware
для domain mapping.

Создайте временную Integration с правами администратора внутри disposable
контейнера и сохраните выведенные значения только в локальном менеджере
секретов или текущем shell:

```bash
docker compose -f docker-compose.shopware-test.yml exec -T shopware \
  bash -lc 'cd /var/www/html && php bin/console integration:create \
    --admin --no-interaction smoke-torgnexa'
```

Команда выводит `SHOPWARE_ACCESS_KEY_ID` и `SHOPWARE_SECRET_ACCESS_KEY` один
раз. Не добавляйте их в репозиторий, issue, CI-лог или скриншот.

## Credentialed smoke

По умолчанию скрипт выполняет только чтения. Для стенда из Compose:

```bash
export SHOPWARE_BASE_URL=http://127.0.0.1:18005
export SHOPWARE_ALLOW_HTTP=1                 # только loopback disposable Docker
export SHOPWARE_HOST_HEADER=localhost        # domain mapping Dockware
export SHOPWARE_CLIENT_ID='temporary-access-key-id'
export SHOPWARE_CLIENT_SECRET='temporary-secret-key'
export SHOPWARE_TEST_SKU=SWDEMO10002
export SHOPWARE_STORE_CURRENCY=EUR
scripts/shopware-smoke.sh
```

Проверяются:

- отказ без bearer и `client_credentials` token exchange;
- bounded catalog/detail read по SKU и обработка актуального JSON:API
  (`data.attributes`) и plain DAL response;
- разрешение валюты и чтение цены;
- stock read;
- bounded orders read и действующий маршрут refunds
  `/api/search/order-transaction-capture-refund`;
- normalized response shape без вывода тел ответов или секретов.

Для disposable записи включаются только явным флагом:

```bash
export SHOPWARE_ALLOW_WRITES=1
scripts/shopware-smoke.sh
```

Smoke временно меняет название/описание товара, цену и stock, выполняет
read-after-write reconciliation и восстанавливает исходные значения в `trap`.
`SHOPWARE_KEEP_CHANGES=1` оставляет их для ручного осмотра — используйте этот
режим только осознанно.

Ожидаемая последняя строка:

```text
Shopware 6 Admin API smoke: all checks passed
```

После проверки удалите только disposable стенд:

```bash
docker compose -f docker-compose.shopware-test.yml down -v
```

## Результат 2026-08-29

Credentialed Docker smoke прошёл на SKU `SWDEMO10002` (EUR): OAuth2, отказ без
авторизации, каталог/detail, currency/price, stock, orders и refunds, записи
товара/цены/stock, read-after-write и автоматический rollback. В процессе
проверки также исправлен connector: он принимает JSON:API `data.attributes`,
`meta.total` и plain entity response, а refunds используют фактический
hyphenated route.

Это подтверждает совместимость с disposable Shopware 6.7 Admin API, но не
сертифицирует конкретный магазин клиента. Внешний live status остаётся
`blocked`, пока не переданы HTTPS endpoint, отдельная Integration credential и
синтетический SKU. Отмена заказа намеренно не выполняется автоматически:
переход state machine необратим. Product create и incoming webhooks остаются
fail-closed согласно [capability-audit.md](capability-audit.md).

Машиночитаемый результат: [live-qualification-status.json](live-qualification-status.json).
