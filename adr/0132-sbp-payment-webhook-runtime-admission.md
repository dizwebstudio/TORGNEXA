# ADR-0132 — admission проверенных webhook СБП

Status: Accepted

## Context

SDK-пакет СБП уже реализует `payments.webhooks`: callback не принимает
заявленный статус на доверии, а повторно читает заказ через
аутентифицированный gateway. Общий public payment receiver уже разрешает
только активный payment account, записывает replay evidence и применяет
канонический переход статуса. Единственная оставшаяся fail-closed граница —
SBP отсутствовал в built-in runtime support contract.

## Decision

Добавить `payments.webhooks` в admission СБП. Использовать существующие
`PaymentGateway`, `sbpHTTP.VerifyWebhook` и
`POST /api/v1/webhooks/payments/{connector_id}/{organization_id}/{workspace_id}/{account_id}`;
нового provider-specific маршрута или Core-ветвления не создавать.

Admission означает repository/runtime readiness, а не подтверждение реального
эквайера. До live qualification остаются обязательными тестовый bank gateway,
актуальный callback contract и проверка эксплуатационных лимитов.

## Alternatives considered

Создавать отдельный SBP webhook route было отклонено: общий payment receiver
уже обеспечивает tenant binding, replay protection и canonical transitions.

## Consequences

Repository admission открывает только проверенный capability path; фактический
банк и эксплуатационные лимиты по-прежнему требуют внешней qualification.

## Migration and data impact

Миграция не требуется: используются существующие payment webhook evidence,
inbox deduplication и outbox records.

## Security and privacy impact

Секрет сертификата остаётся callback-scoped в SecretProvider. Входное тело,
signature header, raw provider response и credential material не сохраняются.
Неизвестный account, ошибка mTLS status re-fetch и повторная доставка получают
одинаковое подтверждение public receiver; evidence deduplicates delivery ID.

## Compatibility impact

Изменение аддитивное: capability появляется только в generated runtime
support/catalog, существующий provider-neutral API и SDK не меняются.

## Operational impact

Health и webhook evidence остаются наблюдаемыми через существующие connector и
audit paths. Откат fail-closed выполняется отключением SBP runtime admission
или capability аккаунта; миграция данных не требуется.
