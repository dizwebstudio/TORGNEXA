# ADR-0117: Рабочее место WMS-оператора и fulfillment из заказа

## Статус

Принято для Task 170. Версия v1: первый production-срез — pick/pack-подобное
исполнение для одного tenant и одного warehouse без provider-specific веток.

## Контекст

В репозитории уже есть reference state machine `internal/platform/wmsexecution`
и canonical `fulfillment_allocations`, но их нельзя использовать как готовое
рабочее место оператора: reference-сервис хранит состояние в памяти, а
allocation ещё не связан с durable WMS task. Marketplace connector в этом
срезе только поставляет order snapshot через уже существующий Order boundary;
WB/Ozon write API, этикетки, DataMatrix/Честный знак и отгрузка остаются
отдельными capability-задачами.

## Решение

1. Создать отдельный durable execution-контур `wms_execution_tasks` и
   `wms_execution_task_events`. Legacy `wms_tasks` и `wms_task_events` не
   переписываются и не становятся вторым источником истины для нового API.
2. Каждая task принадлежит ровно одному tenant, warehouse, SKU и quantity;
   для order fulfillment дополнительно хранит `order_id`, `order_item_id` и
   `fulfillment_allocation_id`. Exact quantity хранится как coefficient/scale
   плюс unit, без float.
3. Жизненный цикл v1: `pending -> in_progress -> completed`, с terminal
   `cancelled` и `exception`. Claim — это optimistic assignment без отдельного
   состояния: повторный claim того же оператора безопасен, чужой assignment
   отклоняется.
4. Любая команда оператора имеет tenant scope, actor, correlation,
   idempotency key и optimistic version. Сканирование записывается в immutable
   task event, изменение task и allocation публикуется через Transactional
   Outbox; consumer-side deduplication остаётся обязательной.
5. `POST /warehouse-tasks/from-order` в одной PostgreSQL-транзакции создаёт
   отсутствующие fulfillment allocations и pick tasks для всех строк заказа.
   Повтор с тем же ключом возвращает прежний набор задач; недостаток ATP,
   inactive warehouse или конфликт существующей allocation откатывают всю
   операцию.
6. Core и API используют capability-neutral термины. Marketplace provider,
   label format и remote ID не попадают в WMS task. Внешняя синхронизация
   заказов и shipment — отдельные connector/fulfillment capabilities.

## Границы и последствия

- PostgreSQL остаётся operational source of truth; RLS включён и enforced на
  обеих новых таблицах. История событий append-only, DELETE/TRUNCATE запрещены.
- В API доступны list/get, claim/start/scan/complete/exception/cancel и
  создание pick tasks из order. Cursor pagination и лимиты обязательны.
- В task events не сохраняются токены, raw provider payloads, Authorization
  headers, полные DataMatrix-коды или лишний PII; barcode хранится только как
  bounded operational reference в scan event.
- Запись в WMS не меняет order status и не списывает on-hand автоматически.
  Reservation создаётся через существующий inventory allocation boundary;
  consumption и shipment будут следующими задачами.
- UI, печать этикеток, агрегация, ChZ/УПД, marketplace orders write и
  fulfillment shipment не входят в Task 170.1–170.4.

## Acceptance criteria

- миграция имеет backup/checksum metadata, RLS, append-only history и
  compatibility notes;
- repository выполняет tenant-scoped idempotent CRUD/commands с optimistic
  concurrency и outbox/audit evidence;
- OpenAPI, runtime route registry и Go/TypeScript/Python SDK генерируются из
  одного контракта;
- order-to-pick orchestration атомарно связывает order item, allocation и
  task, не создаёт duplicate reservation/task при replay и fail-closed при
  недостаточном ATP;
- unit, migration, contract, route-parity и full Go checks проходят.
