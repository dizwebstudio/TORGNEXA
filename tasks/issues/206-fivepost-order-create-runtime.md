# Task 206: 5Post — универсальное создание заказа

## Status

`repository-complete` — 2026-08-31.

## Objective

Подключить bounded однопосылочное создание заказа 5Post к reviewed builtin
runtime по официальному API v7.32.

## Deliverables

- добавить в SDK явные product lines и exact money fields;
- добавить tenant-scoped non-secret configuration для склада, возврата,
  политики невостребованного заказа и barcode enrichment;
- вызвать `POST /api/v3/orders` с одним partner order и cargo;
- проверить product totals, currencies, VAT, payment formula и response
  `code=10` с точными identities;
- добавить OpenAPI, runtime support, registry, deterministic transport/config
  tests, matrix, ADR и architecture-review evidence.

## Scope limits

Тарифный preview, многоместные заказы, курьерская доставка, возвраты,
webhooks, partner vendor objects и живой production qualification не входят в
задачу.

## Source contract

Официальный документ API Партнеров 5post v7.32, обновлён 20.08.2026:
https://fivepost.ru/files/public/API_5post.docx

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, frontend catalog/shell/build checks, architecture review and
`git diff --check`.
