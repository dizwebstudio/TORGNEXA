# Task 197: ПЭК — пакетная печать этикеток заявки

## Status

`repository-complete` — 2026-08-31.

## Objective

Закрыть пакетную печать этикеток одной заявки ПЭК через существующую границу
`logistics.label.read`, сохранив bounded input, host-mediated egress и
проверку результата как PDF.

## Deliverables

- добавить явный формат `multiple_pdf` рядом с `pdf` и `request_pdf`;
- вызвать официальный `/api/v1/order/print/` с `type=multiple` и одним
  числовым кодом груза заявки;
- декодировать только bounded base64-ответ, проверить сигнатуру `%PDF-` и
  вернуть content-addressed opaque reference;
- добавить выбор «Все этикетки заявки» во фронтенде;
- добавить детерминированные adapter/transport tests, ADR, architecture review,
  capability audit, conformance plan и обновить runtime-матрицы.

## Scope limits

Операция печатает только пакет этикеток одной заявки, идентифицированной одним
кодом груза. Она не отменяет сформированный груз, не создаёт отправление и не
выдаёт бинарный PDF или credentials за пределы host transport. Неверный код,
неподдержанный формат, timeout, malformed base64 и не-PDF остаются ошибками.

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`,
`./scripts/check-contracts.sh`, frontend shell/generated-catalog checks,
package-index check and `git diff --check`.
