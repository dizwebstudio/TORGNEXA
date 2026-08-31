# Task 213: «Почта России» — поиск партии по имени

## Status

`repository-complete` — 2026-08-31.

## Objective

Добавить точечный bounded read-only lookup партии по provider name на базе
существующей capability чтения партий.

## Deliverables

- использовать `logistics.batches.read` без новой write-capability;
- вызвать `GET /1.0/batch/{batch-name}` без query/body только для числового ID;
- принять object или массив ровно с одной записи;
- проверить exact batch name, status и shipment count;
- не пропускать состав заказов и raw provider payload;
- добавить OpenAPI, generated SDK, UI, tests, ADR, architecture review и
  qualification evidence.

## Scope limits

Операция только читает одну партию и не изменяет заказы, партию или состояние
провайдера.

## Source contract

Официальная [спецификация API «Отправка»](https://otpravka.pochta.ru/specification),
раздел «Поиск партии по наименованию»: `GET /1.0/batch/{batch-name}`.

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, SDK/catalog/frontend checks, architecture review and
`git diff --check`.
