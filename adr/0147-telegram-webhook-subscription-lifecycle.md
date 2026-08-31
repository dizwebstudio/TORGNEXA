# ADR-0147 — Управляемый lifecycle Telegram webhook

Status: Accepted

## Context

Task 194 принимает проверенные Telegram `channel_post` и
`edited_channel_post`, но endpoint и подписка оставались ручной внешней
настройкой. Из-за этого приложение не могло безопасно включить или снять
доставку для конкретного connector account, а повтор запроса мог привести к
повторной операции у провайдера.

## Decision

Добавить provider-neutral `SocialWebhookController` и две защищённые
операции:

- `PUT /api/v1/social/webhooks/subscription` вызывает Telegram `setWebhook`;
- `DELETE /api/v1/social/webhooks/subscription` сначала вызывает
  `getWebhookInfo`, а затем `deleteWebhook` только при пустом или точно
  совпадающем endpoint.

Обе операции tenant-scoped, требуют активный social account, capability
`social.webhooks`, `connectors.accounts.write`, `Idempotency-Key` и аудит.
Endpoint принимает только HTTPS с допустимым Telegram портом, а секрет
остаётся callback-scoped в SecretProvider. `setWebhook` всегда ограничивает
доставку двумя update-типами, уже поддержанными Task 194.

## Security and privacy impact

Bot token и webhook secret не покидают callback SecretProvider. Endpoint не
может быть использован как provider egress destination: он передаётся только
в Telegram lifecycle API после bounded validation и не сохраняется в audit
summary. Перед удалением выполняется exact-endpoint guard, чтобы один tenant
не снял webhook другой deployment. Ошибка или неоднозначный внешний результат
не переводится в completed operation.

## Compatibility impact

Изменение аддитивное: добавляются SDK interface, OpenAPI operations и
generated clients. Existing inbound webhook route and publication operations
не меняются. Database migration не требуется: используется существующий
tenant-scoped operation receipt.

## Operational impact

Deployment owner должен предоставить публичный HTTPS endpoint и
callback-scoped secret reference. Оператор может повторить тот же запрос по
тому же idempotency key; completed result возвращается без нового вызова
Telegram. Для смены endpoint сначала требуется новая подписка, затем
отписка старого endpoint с точным совпадением.

## Alternatives considered

Оставить lifecycle ручным отвергнуто: приложение не могло проверить владение
endpoint и не имело durable idempotency. Вызывать только `deleteWebhook`
отвергнуто: это позволяло бы удалить чужую активную подписку. Разрешить все
Telegram update-типы отвергнуто: текущий Inbox/outbox bridge квалифицирован
только для channel posts.

## Consequences

Telegram subscription lifecycle становится наблюдаемой и повторяемой
операцией host API, но только для квалифицированной поверхности. Provider
specific transport остаётся внутри Telegram adapter и не проникает в общий
API. Callback actions, direct messages, groups и прочие update-типы остаются
fail-closed.

## Migration and data impact

Миграций нет. Существующие Telegram accounts должны получить
`WebhookSecretReference`, а deployment — endpoint. Operation receipt и audit
используют существующие хранилища; секреты и provider response туда не
попадают.
