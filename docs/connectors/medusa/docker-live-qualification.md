# Medusa v2: Docker и credentialed smoke

## Docker-стенд

В репозитории TORGNEXA нет отдельной копии Medusa: это headless-приложение,
которое собирается из проекта и требует PostgreSQL и Redis. Официальная
инструкция Medusa рекомендует DTC Starter и Compose-файл для backend,
PostgreSQL, Redis и, при необходимости, storefront. Она также предупреждает,
что готовые backend-образы не публикуются — образ собирается локально.

Для воспроизводимой проверки в репозитории есть изолированный
`docker-compose.medusa-test.yml`. Он запускает только backend, PostgreSQL и
Redis (storefront намеренно выключен для небольшой VPS), использует именованные
volumes с префиксом `torgnexa-medusa-test` и слушает порт `19000`. Production
Compose этим стендом не затрагивается.

Сначала создайте отдельный non-production проект:

```bash
git clone https://github.com/medusajs/dtc-starter.git --depth=1 /tmp/medusa-docker-test
cd /tmp/medusa-docker-test
cp apps/backend/.env.template apps/backend/.env
```

Запустите Compose TORGNEXA из корня репозитория (путь к проекту должен быть
абсолютным):

```bash
cd /path/to/torgnexa-codex-kit
MEDUSA_PROJECT_DIR=/tmp/medusa-docker-test \
  docker compose -f docker-compose.medusa-test.yml up -d
MEDUSA_PROJECT_DIR=/tmp/medusa-docker-test \
  docker compose -f docker-compose.medusa-test.yml ps
```

Compose монтирует read-only overlay
`scripts/medusa-docker/medusa-config.ts`: он явно отключает TLS только для
локальной PostgreSQL и передаёт `REDIS_URL`, поэтому стенд не зависает на
проверке pool. Миграции и seed запускаются автоматически. Логи:

```bash
MEDUSA_PROJECT_DIR=/tmp/medusa-docker-test \
  docker compose -f docker-compose.medusa-test.yml logs -f medusa
```

Готовность backend — `http://127.0.0.1:19000/health`; Admin —
`http://127.0.0.1:19000/app`. После проверки удалите только этот стенд:

```bash
MEDUSA_PROJECT_DIR=/tmp/medusa-docker-test \
  docker compose -f docker-compose.medusa-test.yml down -v
```

Не используйте старый community template
`medusajs/docker-medusa` для этой проверки: он собирается на устаревшем
Medusa v1 и не является официально поддерживаемым Docker runtime.

Актуальный официальный вариант установки описан в [Install Medusa with
Docker](https://docs.medusajs.com/learn/installation/docker); он полезен для
production-подготовки, но для этого smoke достаточно Compose-файла репозитория.

Создайте Admin API key типа `secret` в Medusa Admin. В v2 он передаётся как
raw token в `Authorization: Basic <token>`; bearer и publishable key для Admin
smoke не подходят (см. [официальные концепции API
key](https://docs.medusajs.com/resources/commerce-modules/api-key/concepts)).
Создайте один синтетический товар с SKU
`TORGNEXA-MEDUSA-SMOKE`. Коннектор намеренно не создаёт товар, потому что
общий контракт не позволяет безопасно выполнить find-or-create по SKU.

## Credentialed smoke

Скрипт запускается из корня TORGNEXA и не сохраняет токен или ответы магазина:

```bash
export MEDUSA_BASE_URL=http://127.0.0.1:19000
export MEDUSA_API_TOKEN='secret-api-key-from-medusa-admin'
export MEDUSA_TEST_SKU=TORGNEXA-MEDUSA-SMOKE
export MEDUSA_ALLOW_HTTP=1       # только для loopback Docker
scripts/medusa-smoke.sh
```

Для запущенного стенда из этой инструкции используйте порт `19000`. Seed DTC
Starter уже содержит синтетический SKU `SHORTS-M`; заранее создайте Admin user
и secret API key в тестовой базе. Admin user можно создать CLI-командой внутри
контейнера (пароль задайте одноразовый и не коммитьте его):

```bash
MEDUSA_PROJECT_DIR=/tmp/medusa-docker-test \
  docker compose -f docker-compose.medusa-test.yml exec -T medusa \
  sh -lc 'pnpm --filter @dtc/backend exec medusa user \
    -e smoke-admin@example.test -p "replace-with-a-local-password"'
```

Затем создайте secret API key в Admin и передайте ключ в `MEDUSA_API_TOKEN`.
Сам ключ в команды и логи не подставляйте.

Для удалённого staging используйте HTTPS и не задавайте `MEDUSA_ALLOW_HTTP`.
Самоподписанный сертификат разрешается только явно через
`MEDUSA_INSECURE_TLS=1`.

По умолчанию проверяются только чтения:

- отказ `401/403` без ключа и успешный Admin catalog read;
- сопоставление SKU с product/variant и чтение варианта/цены;
- inventory item, location level и доступность write-location;
- при заданном `MEDUSA_TEST_ORDER_ID` — чтение заказа и returns.

Для disposable товара включите записи:

```bash
MEDUSA_ALLOW_WRITES=1 scripts/medusa-smoke.sh
```

Проверяются обновление title/description/status, цены варианта и остатка с
read-after-write reconciliation. Исходные значения восстанавливаются в
`trap`; `MEDUSA_KEEP_CHANGES=1` оставляет их для ручного осмотра. Отмена заказа
не выполняется автоматически: `POST /admin/orders/{id}/cancel` необратима и
требует отдельного согласованного disposable-order теста.

Проверка, выполненная 2026-08-29 на DTC Starter в этом Compose, прошла для
SKU `SHORTS-M`: неавторизованный запрос получил 401, каталог/товар/вариант/
остаток/локация прочитаны, записи товара/цены/остатка прошли read-after-write,
а `trap` восстановил исходные значения (`USD 15`, остаток `1000000`).

Ожидаемая последняя строка успешного запуска:

```text
Medusa v2 Admin REST smoke: all checks passed
```

Synthetic fixture smoke не заменяет проверку Docker-магазина. Docker smoke уже
зафиксирован как PASS в `live-qualification-status.json`, но credentialed
проверка внешнего staging endpoint остаётся `BLOCKED`, пока не переданы
`MEDUSA_BASE_URL`, `MEDUSA_API_TOKEN` и `MEDUSA_TEST_SKU` от отдельного
non-production магазина.
