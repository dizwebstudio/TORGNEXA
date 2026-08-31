# Task 216: «Деловые Линии» — отмена Pre-Alert пакетной заявки

## Status

`repository-complete` — 2026-08-31.

## Objective

Закрыть fail-closed операцию расформирования Pre-Alert пакетной заявки через
официальный API «Деловых Линий», не смешивая её с отменой отдельной
терминальной перевозки или ручным возвратом.

## Deliverables

- добавить capability `logistics.batches.cancel` и контракт операции;
- вызвать `POST /v2/batch_request/cancel.json` с числовым `batchRequestID`;
- принимать только `metadata.status=200` и `data.state=success`;
- добавить approval-bound API route с tenant-scoped idempotency receipt;
- добавить SDK, runtime support, frontend UI, deterministic transport,
  connector и API tests, ADR и qualification evidence.

## Scope limits

Операция расформировывает только созданную текущим пользователем Pre-Alert
пакетную заявку. Отмена отдельного терминального заказа, ручные возвраты,
провайдерская reconciliation и любые другие batch update операции остаются за
пределами задачи.

## Source contract

Официальная [спецификация Pre-Alert «Деловых Линий»](https://dev.dellin.ru/api/ordering/pre-alert/),
раздел «Отмена пакетного заказа».

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, SDK/catalog/frontend checks, architecture review and
`git diff --check`.
