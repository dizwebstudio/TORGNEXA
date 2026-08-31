# Task 194: Telegram — входящие channel-post webhook

## Status

`repository-complete` — 2026-08-31.

## Objective

Закрыть отдельную Telegram inbound-операцию без доверия к непроверенному
payload: принимать только публикации настроенного канала и сохранять
deduplicated evidence через общий Inbox/outbox контур.

## Deliverables

- добавить `social.webhooks` в Telegram manifest и built-in runtime support;
- добавить callback-scoped webhook secret configuration;
- реализовать проверку `X-Telegram-Bot-Api-Secret-Token`, canonical JSON,
  channel/update/message/date bounds и content-addressed delivery identity;
- подключить Telegram к существующему tenant-bound social webhook route;
- обновить OpenAPI, generated catalog, capability audit, conformance plan,
  reconciliation notes и runtime matrices;
- добавить негативные и положительные deterministic tests.

## Safety boundary

Принимаются только `channel_post` и `edited_channel_post` для одного exact
negative `chat_id`. Прямые сообщения, группы, callback queries, subscription
lifecycle и неизвестные update-типы не проходят admission. Секреты остаются в
SecretProvider, а durable replay claim выполняет host-owned Inbox/outbox.

## Verification

Run connector/API/runtime, OpenAPI, SDK, contract, frontend, full Go and
`git diff --check` checks. Credentialed live qualification requires a
non-production Telegram bot, a dedicated channel and a deployment-managed
webhook secret.
