# Marketplace Operations v1 — эксплуатационный контур

## Назначение

Epic 176 (`tasks/issues/223-marketplace-operations-v1.md`) — это сквозной
release gate для marketplace, а не отдельный provider adapter. Он собирает
состояние кабинета, каталога, цен, остатков, заказов, WMS, отгрузок, возвратов,
маркировки, settlement и P&L.

Репозиторная часть v1 control-plane закрыта: матрица доступности, операторский
экран, bounded WB/Ozon order reader и provider-neutral flow находятся в коде.
Полная live qualification конкретных кабинетов остаётся условием выпуска и не
может быть подменена наличием токена или synthetic fixture.

## Что уже работает

- `GET /api/v1/marketplace-operations` показывает tenant-scoped матрицу
  фактических capabilities и состояния `read_only`, `partially_supported`,
  `qualified` и `blocked`.
- `GET /api/v1/marketplace-operations/flows` показывает сохранённые стадии
  workflow и unknown-состояния с cursor pagination. Команды workflow пишутся в
  append-only журнал; повтор с тем же idempotency key не создаёт вторую запись.
- `POST /api/v1/marketplace-operations/flows` создаёт tenant-scoped flow, а
  `POST /api/v1/marketplace-operations/flows/{flow_id}/commands` принимает
  typed-команду текущей стадии. Оба маршрута требуют `Idempotency-Key`; API
  меняет только orchestration projection и не выполняет remote side effect.
- WB и Ozon предоставляют bounded FBS-order reader для reconciliation и
  host sync: provider-owned cursor, дедупликация и нормализованные статусы.
- Материализация remote order в canonical `orders` должна выполняться
  отдельным host importer после проверки offer/SKU mapping. Builder
  `BuildMarketplaceOrderCreate` принимает только полный redacted snapshot с
  canonical OfferID, точными money/quantity/tax и canonical IDs; reader не
  создаёт вторую модель заказа и не объявляет удалённую запись локально
  сохранённой.
- `internal/core/marketplaceoperations` проверяет порядок сквозного сценария
  `account → product → publication → price/stock → order → reserve →
  pick/pack → shipment → return → settlement → P&L`, сохраняет только ссылки
  на канонические домены и оставляет timeout в состоянии `unknown`. Его
  `LifecycleRunner` последовательно вызывает владельцев bounded contexts и
  останавливается на `unknown`/`rejected`, не повторяя внешний write вслепую.
- `GET /api/v1/marketplace-operations/findings` и
  `GET /api/v1/marketplace-operations/flows/{flow_id}/findings` показывают
  tenant-scoped findings: stale data, missing mapping, duplicate order,
  price/stock mismatch, partial response, health и dead-letter состояния.
  `POST /api/v1/marketplace-operations/findings/{finding_id}/actions` пишет
  append-only intent `retry`/`reconcile`/`resolve`; сам endpoint не выполняет
  удалённый побочный эффект.
- Миграции `000048_marketplace_operations_runtime.sql` и
  `000049_marketplace_operations_findings.sql` хранят только
  provider-neutral projection и ссылки на канонические записи; raw payloads,
  токены и credentials в таблицах отсутствуют.
- Каталог/публикация, WMS reservations, возвраты, settlement/P&L и реклама
  используются через существующие bounded contexts, без второй модели заказа
  или товара.

## Статусы поддержки

- `read_only` — разрешены только подтверждённые чтения;
- `partially_supported` — часть операций допущена, остальные явно denied;
- `qualified` — полный заявленный сценарий прошёл synthetic/Docker и
  provider-specific live qualification.

Статус вычисляется по capability и evidence. Наличие manifest, SDK-типа,
credentials или health-check не делает кабинет `qualified`.

## Рабочий поток

```text
account → product → publication → price/stock → order → reserve
→ pick/pack → shipment → return → settlement → P&L
```

Каждый шаг должен иметь tenant scope, operation/idempotency reference,
normalized status, audit lineage и reconciliation path. Операция с timeout
после удалённого принятия получает `unknown`; оператор сначала запускает
reconciliation/status read, а не повторяет write вслепую.

## Перед включением capability

1. Проверить account state, актуальность credentials и required auth scopes.
2. Проверить connector capability и provider qualification evidence.
3. Проверить policy/approval, лимит массовой операции и idempotency key для
   write-sensitive action.
4. Проверить backup/migration gate дочернего runtime slice.
5. Выполнить dry-run или preflight и убедиться, что stale/missing mappings
   видны оператору.

## При сбое

- `timeout`/неизвестный ответ: остановить повторную запись, создать
  `unknown`/`manual_attention`, выполнить status read и reconciliation;
- `rate_limit`: сохранить bounded retry-after и не обходить лимит параллелизмом;
- `reauthorization_required`: отключить затронутые writes, не выводить токен в
  ошибку, обновить credential через SecretProvider;
- mismatch order/stock/price/settlement: не исправлять исторический факт,
  сформировать append-only finding;
- падение worker: восстановить lease безопасным replay; внешнюю операцию с
  неизвестным результатом не повторять без evidence.

Finding создаётся один раз с digest безопасного evidence. Его исходная запись
не меняется: решение оператора добавляется отдельной строкой action journal,
а статус `resolved` вычисляется по журналу. Это сохраняет историю и позволяет
повторить аудит без хранения ответа marketplace.

## Критерий выпуска

Для каждого provider отдельно публикуются capability matrix, redacted
qualification evidence и дата актуальности. До прохождения полного сценария
интерфейс обязан показывать `read_only` или `partially_supported` и скрывать
неразрешённые mutation actions.
