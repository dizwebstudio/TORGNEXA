# Task 178: ПЭК — печатная форма заявки

## Status

`repository-complete` — 2026-08-31.

## Objective

Закрыть ещё один квалифицированный read-маршрут ПЭК через существующую
границу `logistics.label.read`: получать полную печатную форму заявки,
сохраняя строгую валидацию входа, PDF и host-mediated egress.

## Deliverables

- добавить явный формат `request_pdf` рядом с обычным `pdf`;
- вызвать официальный `/api/v1/order/print/` с `type=big` и cargo code;
- декодировать только bounded base64-ответ, проверить media type и сигнатуру
  `%PDF-`, затем вернуть content-addressed opaque reference;
- добавить выбор типа документа во фронтенде;
- добавить детерминированный тест, ADR, architecture review, capability audit,
  conformance plan и обновить runtime-матрицы.

## Scope limits

Обычный `pdf` остаётся запросом этикетки одного груза (`type=simple`). Пакетная
печать (`type=multiple`), отмена сформированного груза, возвраты, вебхуки и
прочие операции записи остаются fail-closed. Сетевой timeout, неверный код,
невалидный base64 или не-PDF не создают фиктивный документ.

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`,
`./scripts/check-contracts.sh`, frontend shell/generated-catalog checks,
package-index check and `git diff --check`.
