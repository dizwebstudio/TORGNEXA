# Task 211: «Почта России» — чтение заказов внутри партии

## Status

`repository-complete` — 2026-08-31.

## Objective

Подключить официальный read-only маршрут списка заказов сформированной партии
через нейтральную bounded capability TORGNEXA.

## Deliverables

- добавить `logistics.batches.orders.read` и нейтральный SDK-порт;
- вызвать `GET /1.0/batch/{batch-name}/shipment` только для числового batch ID;
- передавать bounded `size`, `page`, `sort=ask`;
- отклонять mismatch, дубликаты, malformed rows и ответ сверх лимита;
- не пропускать PII и raw provider payload;
- добавить OpenAPI, generated SDKs, runtime support, UI, tests, ADR,
  architecture review и qualification evidence.

## Scope limits

Операция только читает строки партии. Она не изменяет заказы, партию или
состояние провайдера и не подменяет restore/cancel.

## Source contract

Официальная [спецификация API «Отправка»](https://otpravka.pochta.ru/specification),
раздел «Запрос данных о заказах в партии»: `GET
/1.0/batch/{batch-name}/shipment`.

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, SDK/catalog/frontend checks, architecture review and
`git diff --check`.
