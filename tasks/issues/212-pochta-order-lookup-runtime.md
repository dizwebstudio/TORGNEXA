# Task 212: «Почта России» — поиск одного заказа в партии

## Status

`repository-complete` — 2026-08-31.

## Objective

Подключить официальный read-only поиск заказа по внутреннему ID через
нейтральную bounded capability TORGNEXA.

## Deliverables

- добавить `logistics.orders.read` и нейтральный SDK-порт;
- вызвать `GET /1.0/shipment/{id}` только для числового provider order ID;
- принять ровно одну запись (object или single-item array);
- отклонять другой ID, отсутствующий batch ID и malformed response;
- не пропускать PII и raw provider payload;
- добавить OpenAPI, generated SDKs, runtime support, UI, tests, ADR,
  architecture review и qualification evidence.

## Scope limits

Операция только читает один заказ внутри партии. Она не изменяет заказ или
партию и не подменяет список заказов, restore или cancel.

## Source contract

Официальная [спецификация API «Отправка»](https://otpravka.pochta.ru/specification),
раздел «Поиск заказа в партии по id»: `GET /1.0/shipment/{id}`.

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, SDK/catalog/frontend checks, architecture review and
`git diff --check`.
