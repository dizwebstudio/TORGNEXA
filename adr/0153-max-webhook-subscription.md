# ADR-0153 — подписка webhook MAX

Status: Accepted

## Context

MAX уже имел проверенный adapter для `POST /subscriptions` и
`DELETE /subscriptions`, но reviewed runtime оставлял контроллер доступным
только для Telegram. Из-за этого inbound webhook route работал, а оператор не
мог управлять подпиской из authenticated host API.

## Decision

Разрешить MAX `SocialWebhookController` в builtin runtime. Использовать
существующий `PUT/DELETE /api/v1/social/webhooks/subscription` с tenant-scoped
operation receipt и audit evidence. Adapter фиксирует только
`message_created`, `message_edited` и `message_removed`, получает отдельный
verification secret из SecretProvider и принимает только HTTPS endpoint с
неявным портом 443. Provider response должен содержать явный `success=true`;
неоднозначная запись не повторяется автоматически.

## Security and privacy impact

Маршрут сохраняет account/workspace authorization, capability check,
idempotency и audit boundary. Bot token и verification secret не попадают в
конфигурацию операции, журнал или внешний результат. Callback updates и
произвольные update-типы не расширяются.

## Compatibility impact

Изменение аддитивное: существующие Telegram routes и MAX inbound webhook
verification не меняются. MAX получает только уже определённый SDK controller;
новый public endpoint не добавляется.

## Operational impact

Подписка и отписка выполняются через официальный fixed-host MAX API с текущими
timeout/rate-limit правилами connector transport. При неизвестном исходе
операция остаётся pending/unknown в существующем operation receipt и не
создаёт дубликатную попытку.

## Migration and data impact

Миграция не требуется. Используются существующие account configuration,
SecretProvider, operation receipt и audit record.

## Alternatives considered

Оставить MAX controller SDK-only отвергнуто: это делало подключённый inbound
webhook контур неуправляемым. Создавать MAX-специфичный endpoint отвергнуто:
общий Social subscription route уже содержит нужную approval/idempotency и
tenant boundary.

## Consequences

Оператор может безопасно включить или отключить MAX webhook subscription из
подключённого приложения. Provider-native polling, callback actions и иные
непроверенные update-типы остаются вне admission.
