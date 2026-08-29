# Saleor: Docker и credentialed GraphQL smoke

## Что проверяем

Saleor — GraphQL-only storefront. В отличие от REST-коннекторов, его API
может вернуть HTTP 200 вместе с GraphQL `errors`, поэтому smoke проверяет и
HTTP-код, и тело ответа. Secret/App token передаётся как
`Authorization: Bearer <token>`.

Официальный [Saleor Platform](https://github.com/saleor/saleor-platform)
предназначен для локальной разработки, а не для production. Его README
описывает миграции, `populatedb --createsuperuser` и API на порту 8000; в
репозитории TORGNEXA это сведено к отдельному Compose-файлу только с API,
PostgreSQL и Valkey, чтобы не перегружать небольшую VPS.

## Изолированный Docker-стенд

Запустите из корня TORGNEXA:

```bash
docker compose -f docker-compose.saleor-test.yml up -d
docker compose -f docker-compose.saleor-test.yml ps
```

Файл [docker-compose.saleor-test.yml](../../../docker-compose.saleor-test.yml)
использует официальный образ `ghcr.io/saleor/saleor:3.23`, автоматически
выполняет `migrate` и `populatedb --createsuperuser`, а API публикует на
`http://127.0.0.1:18000/graphql/`. Dashboard, worker, Mailpit и Jaeger не
запускаются: они не нужны для проверки коннектора и потребляют память.

Seed-команда создаёт локального администратора `admin@example.com` с паролем
`admin` (только для disposable стенда). Получите временный bearer-токен через
GraphQL `tokenCreate` или создайте отдельный Saleor App token в Dashboard.
Токен не выводите в логи и не коммитьте.

```bash
login_json=$(curl -fsS http://127.0.0.1:18000/graphql/ \
  -H 'content-type: application/json' \
  --data '{"query":"mutation { tokenCreate(email: \"admin@example.com\", password: \"admin\") { token errors { field message code } } }"}')
export SALEOR_API_TOKEN="$(printf '%s' "$login_json" | \
  python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["tokenCreate"]["token"])')"
```

Проверить seed SKU и warehouse можно GraphQL-запросом. В проверке 2026-08-29
использовались канал `default-channel`, склад `default` и SKU `111223580`;
на другой версии seed-slug может отличаться, поэтому smoke принимает
фактические значения через переменные.

## Credentialed smoke

Скрипт [saleor-smoke.sh](../../../scripts/saleor-smoke.sh) не сохраняет токен
или ответы API и по умолчанию выполняет только чтения:

```bash
export SALEOR_GRAPHQL_URL=http://127.0.0.1:18000/graphql/
export SALEOR_TEST_SKU=111223580
export SALEOR_CHANNEL=default-channel
export SALEOR_WAREHOUSE=default
export SALEOR_ALLOW_HTTP=1       # только для loopback Docker
scripts/saleor-smoke.sh
```

Проверяются отказ без bearer, bounded `productVariants` read, сопоставление
SKU с variant/product, чтение channel price и warehouse stock, а также
разрешение channel/warehouse IDs.

Для disposable стенда можно включить записи:

```bash
export SALEOR_ALLOW_WRITES=1
scripts/saleor-smoke.sh
```

Запись проверяет три реальные мутации product (`sku`, `name`, publication),
цену канала и stock warehouse, затем выполняет read-after-write и
восстанавливает исходные значения. `SALEOR_KEEP_CHANGES=1` оставляет изменения
для ручного осмотра. Создание продукта и входящие webhooks намеренно не
тестируются: они fail-closed в коннекторе.

Для удалённого staging используйте HTTPS и не задавайте `SALEOR_ALLOW_HTTP`;
самоподписанный сертификат разрешается только явно через
`SALEOR_INSECURE_TLS=1`. После проверки удалите только временный стенд:

```bash
docker compose -f docker-compose.saleor-test.yml down -v
```

## Ограничения qualification

### Результат Docker-проверки (2026-08-29)

Изолированный стенд успешно прошёл credentialed smoke на SKU `111223580`,
канале `default-channel` (USD) и складе `default`. Проверены отказ без bearer,
bounded catalog/detail read, разрешение channel/warehouse, записи SKU/name и
publication, цены и остатков, read-after-write и автоматический откат всех
изменений. Итог скрипта: `Saleor GraphQL smoke: all checks passed`.

Это подтверждает работу коннектора с официальным Saleor Platform image в
Docker, но не является проверкой внешнего merchant staging. Стенд после
проверки был остановлен командой `docker compose ... down -v`; production
окружение и его данные не затрагивались. Машиночитаемый результат хранится в
[live-qualification-status.json](live-qualification-status.json).

SDK-конформанс и Docker smoke не заменяют проверку конкретного merchant
staging: версия Saleor, права App token, channel/warehouse и данные магазина
могут отличаться. Внешний live status остаётся `BLOCKED`, пока отдельный
non-production endpoint не пройдёт тот же smoke с реальными credentials.
