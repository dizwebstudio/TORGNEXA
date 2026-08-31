# Task 179: Почта России — bounded чтение партий

## Status

`repository-complete` — 2026-08-31.

## Objective

Закрыть read-only часть работы с партиями Почты России через capability
`logistics.batches.read`: добавить официальный provider route, нейтральную
API-проекцию, UI-проверку и строгие ограничения ответа.

## Deliverables

- добавить `logistics.batches.read` в manifest и built-in runtime support;
- вызвать официальный `GET /1.0/batch` с bounded фильтрами, страницей и размером;
- валидировать уникальный идентификатор партии, статус и количество отправлений;
- вернуть только нейтральные поля без состава заказов и сырых ответов;
- добавить API/transport тесты, UI, ADR, architecture review, audit и conformance;
- оставить формирование партии и передачу в работу fail-closed.

## Scope limits

Операция не формирует, не изменяет и не передаёт партию. Сырой ответ
провайдера, состав отправлений и credentials не попадают в API, события,
журналы или frontend. Размер страницы ограничен 100 записями, а malformed,
duplicate или oversized ответы отвергаются.

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`,
`./scripts/check-contracts.sh`, frontend shell/generated-catalog checks,
package-index check and `git diff --check`.
