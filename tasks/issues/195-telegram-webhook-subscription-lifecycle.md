# Task 195: Telegram — lifecycle подписки webhook

## Status

`repository-complete` — 2026-08-31.

## Objective

Закрыть Telegram webhook subscription lifecycle через официальный Bot API,
сохранив tenant binding, callback-scoped secrets, durable idempotency и
fail-closed удаление чужого endpoint.

## Deliverables

- добавить provider-neutral controller interface и runtime admission;
- реализовать Telegram `setWebhook`, `getWebhookInfo` и `deleteWebhook`;
- ограничить подписку `channel_post` и `edited_channel_post`;
- добавить защищённые PUT/DELETE API operations, audit и replay response;
- обновить OpenAPI, generated SDK, runtime parity и connector documentation;
- добавить deterministic connector, runtime and API tests.

## Safety boundary

Endpoint должен быть HTTPS и проходить bounded URL validation. Bot token и
webhook secret доступны только через SecretProvider callbacks. Unsubscribe
делает provider read-before-delete и не удаляет endpoint, если Telegram
сообщает другой URL. Pending/ambiguous operation не повторяется автоматически.

## Verification

Проверены целевые API/runtime/connector tests. Полный contract, migration,
SDK, frontend и Go gates выполняются после регенерации публичного контракта.
Credentialed live qualification требует непроизводственного Telegram bot,
отдельного канала и deployment-managed HTTPS endpoint.
