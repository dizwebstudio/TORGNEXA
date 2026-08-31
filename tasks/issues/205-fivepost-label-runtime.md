# Task 205: 5Post — PDF-этикетка

## Status

`repository-complete` — 2026-08-31.

## Objective

Подключить bounded PDF label read 5Post к reviewed builtin runtime по
официальному API v7.32.

## Deliverables

- допустить `logistics.label.read` в runtime registry;
- вызвать `POST /api/v1/orderLabels/byOrderId?format=PDF` для одного UUID;
- проверить `Content-Type: application/pdf` и сигнатуру `%PDF-`;
- вернуть только content-addressed digest reference;
- добавить deterministic success/non-PDF transport tests, registry, contract,
  matrix, ADR и architecture-review docs.

## Scope limits

ZIP/multi-order labels, label-status polling, partner-generated labels,
shipment creation, tracking history, callbacks и возвраты не входят в задачу.

## Source contract

Официальный документ API Партнеров 5post v7.32, обновлён 20.08.2026:
https://fivepost.ru/files/public/API_5post.docx

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, frontend catalog/shell/build checks, architecture review and
`git diff --check`.
