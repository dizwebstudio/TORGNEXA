# Shopify: Docker protocol smoke и Dev Store qualification

## Важное ограничение

Shopify — SaaS-платформа: официального self-hosted Docker-магазина нет. В
репозитории поэтому используется stateful protocol double. Он повторяет
документированные Admin REST request/response shapes и позволяет проверить
транспорт, авторизацию, записи и reconciliation без доступа к merchant data.
Это не Shopify store и не заменяет проверку через Shopify Dev Dashboard.

Официальные dev stores предназначены для разработки и тестирования и требуют
Shopify Partner/Dev Dashboard доступа; Shopify описывает их создание в
[Dev stores](https://shopify.dev/docs/apps/build/stores/development-stores).
Admin REST API уже считается legacy для новых приложений, а текущая доступная
stable-версия на момент проверки — `2026-07`; версия и заголовок ответа
проверяются smoke-скриптом. См. [REST Admin API reference](https://shopify.dev/docs/api/admin-rest/latest)
и [API versioning](https://shopify.dev/docs/api/usage/versioning).

## Docker protocol double

Запустите из корня TORGNEXA:

```bash
docker compose -f docker-compose.shopify-test.yml up -d
docker compose -f docker-compose.shopify-test.yml ps
```

Compose поднимает только Python protocol double на `127.0.0.1:18001`; он не
подключается к Shopify и не требует секретов. Seed содержит синтетический
магазин, товар `TORGNEXA-SHOPIFY-001`, склад `TORGNEXA Demo Warehouse`, заказ
`5001` и refund `7001`.

Credentialed smoke:

```bash
SHOPIFY_BASE_URL=http://127.0.0.1:18001 \
SHOPIFY_API_TOKEN=shopify-local-token \
SHOPIFY_TEST_SKU=TORGNEXA-SHOPIFY-001 \
SHOPIFY_ALLOW_HTTP=1 \
SHOPIFY_ALLOW_WRITES=1 \
scripts/shopify-smoke.sh
```

Проверяются отказ без `X-Shopify-Access-Token`, health `/shop.json`, текущая
API-версия, locations, product/variant/inventory mapping, цены и остатки,
orders/refunds, product title/body/status write, variant price write,
inventory-level write, read-after-write и автоматическое восстановление
синтетических значений. Токен и response bodies не выводятся.

После проверки удалите только временный double:

```bash
docker compose -f docker-compose.shopify-test.yml down -v
```

## Проверка реального Shopify Dev Store

Создайте development store и приложение через Dev Dashboard, выдайте только
нужные Admin API scopes и добавьте синтетический товар с SKU
`TORGNEXA-SHOPIFY-SMOKE`. Для REST connector нужен access token приложения.
Запускайте smoke без `SHOPIFY_ALLOW_HTTP`:

```bash
export SHOPIFY_BASE_URL=https://your-shop.myshopify.com
export SHOPIFY_API_TOKEN='token-from-the-dev-dashboard'
export SHOPIFY_TEST_SKU=TORGNEXA-SHOPIFY-SMOKE
scripts/shopify-smoke.sh
```

Для disposable Dev Store можно явно включить записи:

```bash
SHOPIFY_ALLOW_WRITES=1 scripts/shopify-smoke.sh
```

Исходные product title/body/status, variant price и inventory quantity
восстанавливаются автоматически. `SHOPIFY_KEEP_CHANGES=1` оставляет изменения
для ручной проверки. Операции cancel/close/reopen заказа smoke намеренно не
выполняет: они меняют жизненный цикл заказа и не имеют безопасного полного
rollback.

Если Shopify вернул другой `X-Shopify-API-Version`, проверка завершается
ошибкой: это означает, что запрошенная версия недоступна и платформа выполнила
fall-forward. В таком случае сначала обновите pinned API version и контракт,
а не принимайте молчаливый fallback.

## Результат

Docker protocol smoke прошёл 2026-08-29 на API `2026-07`; результат записан в
[live-qualification-status.json](live-qualification-status.json). SDK report
Shopify остаётся 13/13 PASS, но внешний live status остаётся `BLOCKED` до
проверки реального non-production Dev Store с приложением, scopes и синтетическим
SKU.
