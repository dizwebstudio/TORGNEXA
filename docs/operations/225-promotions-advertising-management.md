# Акции и реклама: эксплуатация Task 225

## Что доступно

Раздел `/advertising` содержит шесть рабочих представлений:

- эффективность и нормализованные расходы из Task 220;
- кампании;
- акции с календарём, скидкой, subsidy, floor и minimum margin;
- ставки и бюджеты с typed bid unit и caps;
- массовые операции до 1 000 SKU;
- сверка и журнал операций.

Изменение выполняется по цепочке:

`preview → floor/margin/freshness guard → approval → idempotent intent → qualification → read-after-write → reconciliation`.

MCP имеет только `commerce.marketplace.growth.preview`. Он не может выдать
approval или применить операцию.

## Состояния

Preview показывает `ready`, `approval_required` или `blocked`. Каждая строка
имеет решение `applied`, `rejected`, `unknown` либо `manual_attention` и список
причин. Durable operation использует состояния `accepted`, `applied`,
`rejected`, `conflict`, `rate_limited`, `unknown`, `manual_attention` и
`qualification_required`.

`qualification_required` означает: локальная проверка и approval прошли,
однако для конкретного connector account ещё нет актуального credentialed
evidence. Это не подтверждение изменения в кабинете.

## API

- `GET /api/v1/marketplace-growth/rules?channel_id=&limit=` — правила и окна участия;
- `POST /api/v1/marketplace-growth/previews` — dry-run расчёт до 1 000 SKU;
- `GET /api/v1/marketplace-growth/previews/{preview_id}` — immutable evidence;
- `GET /api/v1/marketplace-growth/operations?limit=` — журнал intents;
- `POST /api/v1/marketplace-growth/operations` — apply после `Approval-Request-ID` и `Idempotency-Key`;
- `GET /api/v1/marketplace-growth/operations/{operation_id}` — состояние intent;
- `GET|POST /api/v1/marketplace-growth/reconciliation` — drift и read-after-write;
- `GET|POST /api/v1/marketplace-growth/kill-switch` — tenant control.

Суммы передаются в minor units, ставки/маржа — в integer basis points.
Preview не отправляет запросов провайдеру. Повтор apply с тем же ключом
возвращает исходный intent; другой digest с тем же ключом даёт conflict.

## PostgreSQL и безопасность

Migration 53 создаёт tenant-scoped rules, previews, operations, drift и
kill-switch. Preview/rules/drift — append-only; размер JSON ограничен. Все
таблицы имеют forced RLS и требуют `app.organization_id`/
`app.workspace_id`. В evidence не попадают токены, raw HTTP body, URL или PII.

Перед migration нужен проверенный backup PostgreSQL. При инциденте включите
kill switch, отключите `promotions.manage`/`ads.manage`, drain worker и
разбирайте `unknown` через reconciliation. Финансовый ledger не редактируется
для исправления drift.

## Qualification

Synthetic gate проверяет расчёт effective price, floor/margin block, stale
facts, 1 000-SKU bound, approval binding, idempotency, unknown outcome,
reconciliation, kill switch и UI contract. Отдельный release gate должен
сохранить credentialed sandbox/live evidence для официального promotion и
advertising write конкретного marketplace. Пока он не пройден, WB/Ozon не
получают ложный статус полноценного remote management.
