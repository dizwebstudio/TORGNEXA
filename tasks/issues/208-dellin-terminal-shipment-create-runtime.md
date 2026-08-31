# Task 208: «Деловые Линии» — terminal-to-terminal shipment create

## Status

`repository-complete` — 2026-08-31.

## Objective

Расширить уже квалифицированное создание отправления «Деловых Линий» bounded
маршрутом терминал → терминал через официальный `POST /v2/request.json`.

## Deliverables

- добавить необязательный `sender_terminal_id` в tenant-scoped runtime config;
- принимать числовой `pickup_point_ref` как терминал получателя только для
  «Деловых Линий»;
- формировать provider payload с двумя явными terminal references без адресных
  объектов и сохранить существующий address-to-address payload;
- показать настройку терминала получателя в UI и обновить runtime-generated
  catalogs;
- добавить deterministic adapter/connector tests, ADR, conformance evidence и
  обновить матрицы qualification.

## Scope limits

Терминальная отмена, ручные возвраты, адресно-терминальные и
терминал-адресные гибридные маршруты не входят в задачу. Адресные поля общей
нейтральной команды остаются обязательными до отдельного изменения контракта,
но в terminal-to-terminal payload не передаются.

## Source contract

Официальная документация API «Деловых Линий»: [пример оформления заказа](https://dev.dellin.ru/api/examples/request/),
включая `delivery.derival.variant=terminal`, `derival.terminalID`,
`arrival.variant=terminal` и `arrival.terminalID`.

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, frontend catalog/shell/build checks, architecture review and
`git diff --check`.
