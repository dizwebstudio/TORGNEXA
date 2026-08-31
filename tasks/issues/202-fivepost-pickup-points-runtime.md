# Task 202: 5Post — справочник ПВЗ

## Status

`repository-complete` — 2026-08-31.

## Objective

Подключить bounded read-only справочника пунктов выдачи 5Post к reviewed
builtin runtime по официальному API v7.32, сохранив callback-scoped secret,
tenant boundary и provider-neutral `PickupPoint` contract.

## Deliverables

- заменить устаревший credential probe на официальный JWT exchange;
- допустить только `pickup.points.read` в runtime registry;
- вызвать `POST /api/v1/pickuppoints/query` с bounded page size;
- нормализовать только обязательные поля ПВЗ и отфильтровать страну/город;
- добавить deterministic transport, registry, contract, matrix, ADR и
  architecture-review tests/docs.

## Scope limits

Тарифы, создание/отмена отправления, tracking, этикетки, callbacks и возвраты
не входят в Task 202 и остаются fail-closed до отдельных bounded задач.

## Source contract

Официальный документ API Партнеров 5post v7.32, обновлён 20.08.2026:
https://fivepost.ru/files/public/API_5post.docx

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, frontend catalog/shell/build checks, architecture review and
`git diff --check`.
