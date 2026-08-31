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

## Security impact

Секрет сертификата остаётся callback-scoped в SecretProvider. Входное тело,
signature header, raw provider response и credential material не сохраняются.
Неизвестный account, ошибка mTLS status re-fetch и повторная доставка получают
одинаковое подтверждение public receiver; evidence deduplicates delivery ID.

## Compatibility

Изменение аддитивное: capability появляется только в generated runtime
support/catalog, существующий provider-neutral API и SDK не меняются.
