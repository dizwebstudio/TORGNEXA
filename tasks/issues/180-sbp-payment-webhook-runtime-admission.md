# Task 180: СБП — admission проверенных payment webhook

## Status

`repository-complete` — 2026-08-31.

## Objective

Включить уже реализованный SBP `PaymentWebhookVerifier` в production
runtime-support contract. Общий публичный payment webhook receiver должен
маршрутизировать callback СБП так же, как остальные допущенные payment rails:
с tenant/account resolution, mTLS-перепроверкой статуса, replay-deduplication
и canonical payment transition.

## Deliverables

- добавить `payments.webhooks` в SBP built-in runtime support;
- сохранить общий endpoint и provider-neutral обработчик без отдельной
  provider-ветки в Core;
- проверить admission через registry и существующую route-level webhook цепочку;
- синхронизировать frontend/runtime catalog, матрицы, ADR, architecture review
  и provider evidence.

## Security and scope limits

Тело callback и optional signature header не являются источником истины.
`sbpHTTP.VerifyWebhook` извлекает только идентификатор заказа, запрашивает его
статус по account-scoped mTLS-каналу и возвращает нормализованное событие.
Публичный receiver не раскрывает результат разрешения account или верификации,
записывает только digest/evidence и применяет лишь допустимый переход платежа.
Настоящая доставка callback от acquiring bank и его актуальная спецификация
требуют отдельной live qualification.

## Verification

Run targeted SBP/runtime tests, `gofmt`, full `go test ./...`, `go vet ./...`,
`./scripts/check-contracts.sh`, generated frontend catalog checks and
`git diff --check`. No OpenAPI operation or migration is needed: the generic
public payment webhook endpoint already exists.
