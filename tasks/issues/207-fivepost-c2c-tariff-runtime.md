# Task 207: 5Post — C2C тариф

## Status

`repository-complete` — 2026-08-31.

## Objective

Подключить bounded C2C tariff preview 5Post к reviewed builtin runtime по
официальному API v7.32.

## Deliverables

- добавить optional point UUID, date and money fields в neutral rate request;
- вызвать `POST /api/v1/tariff/c2c` с весом в миллиграммах;
- нормализовать `paymentWithVat` и delivery days в `RateQuote`;
- добавить registry/support/frontend route and deterministic tests;
- обновить OpenAPI, matrix, ADR и architecture-review evidence.

## Scope limits

Тарифы кроме C2C, курьерская доставка, сохранение provider response и live
qualification не входят в задачу.

## Source contract

Официальный документ API Партнеров 5post v7.32, обновлён 20.08.2026:
https://fivepost.ru/files/public/API_5post.docx

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, frontend catalog/shell/build checks, architecture review and
`git diff --check`.
