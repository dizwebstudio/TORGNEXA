# Task 204: 5Post — отмена заказа

## Status

`repository-complete` — 2026-08-31.

## Objective

Подключить bounded approval-bound отмену одного заказа 5Post по официальному
API v7.32.

## Deliverables

- допустить `logistics.shipment.cancel` в runtime registry;
- вызвать `DELETE /api/v2/cancelOrder/byOrderId/{orderId}` после callback-scoped
  JWT exchange;
- требовать UUID provider order и непустой host idempotency key;
- принимать только ответ с `error=false` и сохранять корректный cancelled
  projection;
- добавить deterministic success/error transport tests, registry, contract,
  matrix, ADR и architecture-review docs.

## Scope limits

Создание заказа, отмена по senderOrderId, асинхронная история, tracking,
этикетки, callbacks и возвраты не входят в задачу.

## Source contract

Официальный документ API Партнеров 5post v7.32, обновлён 20.08.2026:
https://fivepost.ru/files/public/API_5post.docx

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, frontend catalog/shell/build checks, architecture review and
`git diff --check`.
