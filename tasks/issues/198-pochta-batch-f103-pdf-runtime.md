# Task 198: Почта России — форма Ф103 партии

## Status

`repository-complete` — 2026-08-31.

## Objective

Закрыть генерацию формы Ф103 для одной партии Почты России через существующую
границу `logistics.label.read`, сохранив bounded input, host-mediated egress и
проверку результата как PDF.

## Deliverables

- добавить явный формат `batch_f103_pdf`;
- вызвать официальный `GET /1.0/forms/{batch-name}/f103pdf` для одного
  числового номера партии;
- проверить media type и сигнатуру `%PDF-`, вернуть только
  content-addressed opaque reference;
- добавить выбор «Форма Ф103 партии» во фронтенде;
- добавить deterministic adapter/transport tests, ADR, architecture review,
  capability audit, conformance plan и обновить runtime-матрицы.

## Scope limits

Операция только читает PDF одной партии. Она не меняет партию, не передаёт
состав заказов или credentials за пределы host transport и не включает другие
документы Почты России. Неверный номер партии, timeout, не-PDF и malformed
ответ остаются ошибками.

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`,
`./scripts/check-contracts.sh`, frontend shell/generated-catalog checks,
package-index check and `git diff --check`.
