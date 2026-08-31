# Task 200: MAX — редактирование и удаление сообщений

## Status

`repository-complete` — 2026-08-31.

## Objective

Закрыть оставшиеся MAX `social.post.edit` и `social.post.delete` через
существующий approval-bound Social API и runtime, сохранив tenant isolation,
immutable remote receipt и fail-closed обработку неоднозначных записей.

## Deliverables

- добавить `social.post.edit` и `social.post.delete` в manifest и runtime support;
- реализовать `PUT /messages?message_id=...` и
  `DELETE /messages?message_id=...` в MAX adapter;
- повторно валидировать text/media/buttons и released uploads перед edit;
- принимать только явный `success=true` и отклонять чужой канал;
- подключить MAX к reviewed registry и общему Social UI;
- добавить deterministic adapter, runtime admission, ADR, architecture review,
  capability audit, conformance plan и обновить матрицы.

## Scope limits

Изменение и удаление выполняются только для одного уже опубликованного
сообщения из immutable remote receipt и только после существующего approval.
Новые комментарии, callback actions, webhook subscription и provider-native
workflow не включаются. Timeout/5xx не ретраятся автоматически.

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`,
`./scripts/check-contracts.sh`, frontend catalog/shell/build checks,
SDK checks, architecture review and `git diff --check`.
