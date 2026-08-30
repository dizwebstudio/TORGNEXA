# Task 170 — Рабочее место WMS-оператора и marketplace fulfillment

## Статус

`done` — вертикальный срез 170.1 → 170.2 → 170.3 → 170.4 завершён
2026-08-30.

## Цель

Связать canonical order и fulfillment allocation с durable WMS tasks, чтобы
оператор мог получить очередь, забрать задачу, начать работу, сканировать
товар и закрыть или эскалировать исключение. Первая версия provider-neutral и
ориентирована на FBS-подобный pick-flow; полноценные marketplace orders,
этикетки, ChZ и shipment остаются отдельными задачами.

## Подзадачи

### 170.1 — ADR, границы и policy matrix

- Зафиксировать владельцев Order, Inventory/Fulfillment и WMS Execution.
- Утвердить состояния task, claim semantics, optimistic version, idempotency и
  правила exception/cancel.
- Отделить текущий repository-complete allocation от будущих marketplace
  order/shipment connector capabilities.

**Acceptance:** ADR-0117 и этот task описывают границы, инварианты,
неподдерживаемые операции и критерии rollback без provider branches.

### 170.2 — Durable WMS execution model

- Добавить tenant-scoped task, immutable task event и индексы очереди.
- Связать task с order/order item/allocation, warehouse и exact quantity.
- Включить forced RLS, append-only history, idempotency uniqueness и
  Transactional Outbox/audit integration.

**Acceptance:** PostgreSQL repository сохраняет и читает task только в
  tenant scope, повтор команды не удваивает эффект, version conflict не теряет
  сканирование.

### 170.3 — Operator API and SDK

- Добавить list/get/history и lifecycle commands под `/api/v1`.
- Проверять authn → tenant scope → permission (`wms.read`/`wms.write`),
  bounded input, idempotency и optimistic version.
- Обновить OpenAPI и сгенерировать все public SDK artifacts.

**Acceptance:** route parity, contract checker и SDK drift check видят каждую
операцию; API не раскрывает SQL-модель, секреты или cross-tenant данные.

### 170.4 — Order → allocation → pick task

- Добавить атомарную команду создания pick tasks из заказа для выбранного
  warehouse.
- Для каждой строки использовать существующий allocation invariant и ATP;
  task должен ссылаться на созданную или уже matching allocation.
- Опубликовать allocation/task changes через outbox и сделать replay по
  batch idempotency key безопасным.

**Acceptance:** успешный заказ даёт по одной task на строку, insufficient ATP,
inactive warehouse и terminal order откатывают batch, повтор возвращает тот же
результат.

## Не входит в эту задачу

WB/Ozon orders write, labels, barcode/GTIN master data, Честный знак,
aggregation, shipment confirmation, marketplace status pushes, operator UI и
автоматическое списание on-hand.

## Проверки

`gofmt`, `go test ./...`, `go vet ./...`, `./scripts/check-contracts.sh`,
`make sdk-check`, `make architecture`, `make migrations`.
