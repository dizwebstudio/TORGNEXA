# Task 210: «Почта России» — возврат заказов из партии в «Новые»

## Status

`repository-complete` — 2026-08-31.

## Objective

Подключить официальный `POST /1.0/user/backlog`, который возвращает заказы из
сформированной партии в список «Новые», через нейтральный approval-bound
маршрут TORGNEXA.

## Deliverables

- добавить capability `logistics.orders.restore` и нейтральные SDK-порты;
- вызвать официальный endpoint Почты России только для 1–100 числовых order ID;
- отклонять provider errors, частичные ответы и несовпадающий набор `result-ids`;
- сохранить tenant-scoped approval, idempotency receipt и callback-scoped secrets;
- добавить OpenAPI, generated SDKs, runtime support, UI, deterministic tests,
  ADR, conformance evidence и обновить матрицы.

## Scope limits

Операция возвращает заказ в backlog, но не отменяет его и не создаёт возвратную
посылку. Другие операции партий и документов остаются fail-closed.

## Source contract

Официальная [спецификация API «Отправка»](https://otpravka.pochta.ru/specification),
раздел «Возврат заказов в “Новые”»: `POST /1.0/user/backlog`, массив внутренних
идентификаторов и ответ с `result-ids`/`errors`.

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, SDK/catalog/frontend checks, architecture review and
`git diff --check`.
