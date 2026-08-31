# ADR-0146 — Приём входящих Telegram webhook

Status: Accepted

## Context

Telegram уже имел проверенный Bot API transport для публикаций, но входящие
обновления оставались fail-closed. Нельзя принимать тело webhook на доверии:
оно приходит без пользовательской аутентификации и может повторяться после
сетевого сбоя.

## Decision

Добавить `social.webhooks` в runtime только для Telegram и использовать
существующий публичный маршрут
`POST /api/v1/webhooks/social/{connector_id}/{organization_id}/{workspace_id}/{account_id}`.
Host извлекает `X-Telegram-Bot-Api-Secret-Token`, а коннектор сравнивает его
с отдельным callback-scoped `WebhookSecretReference` в constant time.

Принимаются только `channel_post` и `edited_channel_post` для точно
настроенного канала. Коннектор канонизирует JSON, проверяет update/message
идентификаторы и дату, формирует content-addressed delivery ID и передаёт
минимизированный claim в host-owned Inbox/outbox deduplicator. Subscription
lifecycle, callback queries, direct messages, groups и прочие update-типы
остаются fail-closed.

## Security and privacy impact

Bot token и webhook secret не попадают в событие, лог или API-ответ. Роут
единообразно подтверждает внешнюю доставку даже при ошибке верификации, а
только проверенный claim может попасть в tenant-scoped Inbox и
transactional outbox. Канал и workspace проверяются независимо до передачи
в коннектор.

## Compatibility impact

Изменение аддитивное: capability и runtime-support projection расширяются,
а существующий provider-neutral SocialWebhookReceiver и публичный host route
переиспользуются. Миграция базы данных не нужна.

## Operational impact

Подписка Telegram и endpoint должны быть настроены владельцем deployment.
Нужны отдельный секрет не короче 16 символов и непроизводственный канал для
live qualification. Ошибка Inbox/outbox не должна превращаться в фиктивно
успешную бизнес-операцию.

## Alternatives considered

Разрешить webhook непосредственно в общем HTTP-слое отвергнуто: это вынесло
бы имена заголовков и правила доверия провайдера из коннектора. Принимать все
типы Telegram updates также отвергнуто: для них нет согласованного доменного
смысла и безопасного маршрута обработки в текущем host Inbox/outbox.

## Consequences

Положительный результат ограничен публикациями из одного настроенного канала,
а повторная доставка становится детерминированным no-op через host-owned
deduplicator. Для остальных update-типов и жизненного цикла подписки по-прежнему
возвращается только внешний transport acknowledgement без бизнес-эффекта.

## Migration and data impact

Миграций PostgreSQL не требуется. Для включения операции нужно добавить
callback-scoped secret reference в конфигурацию Telegram-аккаунта и настроить
endpoint у deployment-владельца. В Inbox/outbox сохраняется только
минимизированный claim и fingerprint; исходный payload не становится
операционным доменным объектом.
