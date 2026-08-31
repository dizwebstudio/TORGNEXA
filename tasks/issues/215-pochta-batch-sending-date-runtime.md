# Task 215: «Почта России» — изменение даты передачи партии

## Status

`repository-complete` — 2026-08-31.

## Objective

Закрыть fail-closed операцию изменения дня передачи сформированной партии
через проверенный официальный endpoint «Отправки» без раскрытия сырого
ответа провайдера.

## Deliverables

- добавить capability `logistics.batches.sending_date.write`;
- вызвать `POST /1.0/batch/{batch-name}/sending/YYYY/MM/DD` без body/query;
- принимать пустое успешное подтверждение или JSON без `error-code`;
- нормализовать только batch ID, дату, `UPDATED`/`updated=true` и время;
- добавить approval-bound API route, tenant-scoped idempotency receipt, SDK,
  runtime support, frontend UI, deterministic tests и qualification evidence.

## Scope limits

Операция применяется только к активному logistics account Почты России и
сформированной партии. Любой provider error, malformed response,
несовпадение партии или неоднозначный исход остаётся ошибкой/pending; сырой
ответ и credentials не сохраняются.

## Source contract

Официальная [спецификация API «Отправка»](https://otpravka.pochta.ru/specification),
раздел «Изменение дня отправки в почтовое отделение».

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, SDK/catalog/frontend checks, architecture review and
`git diff --check`.
