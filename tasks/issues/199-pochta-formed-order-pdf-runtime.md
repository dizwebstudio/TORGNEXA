# Task 199: Почта России — форма сформированного заказа

## Status

`repository-complete` — 2026-08-31.

## Objective

Закрыть получение печатной формы уже сформированного заказа Почты России
через существующую границу `logistics.label.read`, сохранив bounded input,
host-mediated egress и проверку результата как PDF.

## Deliverables

- добавить явный формат `formed_order_pdf`;
- вызвать официальный `GET /1.0/forms/{order-id}/forms` для одного
  числового ID заказа;
- передать бумажный формат и текущую UTC-дату;
- проверить media type и сигнатуру `%PDF-`, вернуть только
  content-addressed opaque reference;
- добавить выбор формы сформированного заказа во фронтенде;
- добавить deterministic adapter/transport tests, ADR, architecture review,
  capability audit, conformance plan и обновить runtime-матрицы.

## Scope limits

Операция только читает PDF одного заказа после формирования партии. Она не
меняет заказ или партию, не передаёт состав заказа или credentials за пределы
host transport и не включает другие документы Почты России. Неверный ID,
timeout, не-PDF и malformed ответ остаются ошибками.

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`,
`./scripts/check-contracts.sh`, frontend shell/generated-catalog checks,
package-index check and `git diff --check`.
