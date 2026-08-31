# Task 209: «Деловые Линии» — отмена забора от адреса

## Status

`repository-complete` — 2026-08-31.

## Objective

Подключить второй официальный вариант отмены «Деловых Линий» —
`POST /v3/orders/cancel_pickup.json` — через существующий approval-bound
shipment cancellation workflow.

## Deliverables

- расширить нейтральную команду отмены необязательным режимом `delivery` или
  `pickup`, сохранив `delivery` по умолчанию;
- сохранить legacy digest для режима `delivery`, выделив отдельную
  idempotency-идентичность только для нового режима `pickup`;
- передать режим через tenant-scoped outbox event в worker без хранения
  credentials или provider payload;
- вызвать для Dellin правильный официальный endpoint и нормализовать ответ в
  `cancellation_pending`;
- добавить OpenAPI/event schema, UI-переключатель, SDK regeneration,
  deterministic transport/worker tests и qualification evidence.

## Scope limits

Отмена терминального заказа, ручные возвраты и финальное подтверждение решения
перевозчика не входят в задачу. `pickup` означает отмену забора от адреса, а не
отмену терминального заказа.

## Source contract

Официальная документация API «Деловых Линий»: [отмена заказа и доставки груза](https://dev.dellin.ru/api/order/cancel/),
включая `POST /v3/orders/cancel_pickup.json`, числовой `orderID`,
`requester=sender` и асинхронный ответ `data.status=success`.

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, frontend catalog/shell/build checks, architecture review and
`git diff --check`.
