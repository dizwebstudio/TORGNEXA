# Task 203: 5Post — чтение статуса заказа

## Status

`repository-complete` — 2026-08-31.

## Objective

Подключить bounded single-order status read 5Post к reviewed builtin runtime
по официальному API v7.32.

## Deliverables

- допустить `logistics.track.read` в runtime registry;
- вызвать официальный `POST /api/v1/getOrderStatus` после callback-scoped JWT
  exchange;
- отправлять и принимать ровно один provider order ID;
- нормализовать статус, partner tracking reference и UTC change date;
- добавить deterministic transport, registry, contract, matrix, ADR и
  architecture-review tests/docs.

## Scope limits

`executionStatus`, `errorDesc`, history, callbacks, создание, отмена и
этикетки не входят в задачу и не проецируются в Core.

## Source contract

Официальный документ API Партнеров 5post v7.32, обновлён 20.08.2026:
https://fivepost.ru/files/public/API_5post.docx

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, frontend catalog/shell/build checks, architecture review and
`git diff --check`.
