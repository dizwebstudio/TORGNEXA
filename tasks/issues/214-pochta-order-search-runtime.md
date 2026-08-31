# Task 214: «Почта России» — поиск заказа по номеру магазина

## Status

`repository-complete` — 2026-08-31.

## Objective

Добавить bounded read-only поиск заказов Почты России по назначенному
магазином номеру без раскрытия персональных данных и сырого ответа провайдера.

## Deliverables

- добавить отдельную capability `logistics.orders.search`;
- вызвать `GET /1.0/backlog/search?query=...` через host-side transport;
- ограничить выдачу максимум 100 строками и проверить точное совпадение номера;
- нормализовать только ID заказа, номер магазина, optional batch/barcode,
  статус и UTC-время наблюдения;
- добавить OpenAPI, generated SDK, runtime support, frontend UI и labels;
- добавить transport, connector, registry и API tests, ADR, architecture review
  и qualification evidence.

## Scope limits

Операция только читает backlog и не меняет заказ, партию или состояние
провайдера. Address, recipient, raw payload и credentials не пересекают
границу коннектора. Approval и operation receipt для этого read-only маршрута
не требуются.

## Source contract

Официальная [спецификация API «Отправка»](https://otpravka.pochta.ru/specification),
раздел поиска заказа по назначенному магазином идентификатору:
`GET /1.0/backlog/search` с query `query`.

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, SDK/catalog/frontend checks, architecture review and
`git diff --check`.
