# ADR-0166 — поиск заказа Почты России по номеру магазина

Status: Accepted

## Context

API «Отправка» предоставляет отдельный read-only поиск backlog-заказов по
идентификатору, назначенному магазином. Этот ответ может содержать адреса,
получателя и другие provider-specific поля, поэтому он не должен переходить
в Core или SDK как raw JSON.

## Decision

Добавить отдельную capability `logistics.orders.search` и host route
`GET /api/v1/logistics/orders/search`. Runtime вызывает только официальный
`GET /1.0/backlog/search?query={external_id}` с tenant-scoped credentials,
ограничивает выдачу 100 строками, проверяет exact merchant order number и
возвращает безопасную проекцию `LogisticsOrderSummary`.

Проекция содержит provider order ID, merchant external ID, optional batch ID,
optional tracking number, normalized status и UTC observation time. Дубликаты,
malformed references, mismatched external IDs и oversized responses остаются
provider failure. Операция read-only, поэтому approval и idempotency receipt не
добавляются.

## Security and privacy impact

Маршрут требует authenticated workspace scope, `connectors.read`, активный
logistics account и enabled `logistics.orders.search`. Provider credentials
читаются только через SecretProvider. Адреса, получатели, raw payload и
секреты не выходят из host transport.

## Compatibility impact

Изменение аддитивное: новая capability и OpenAPI/SDK operation не меняют
существующие order lookup, batch read, restore или cancellation semantics.

## Migration and data impact

Миграция не требуется: операция read-only и не создаёт записи.

## Operational impact

Оператор может найти backlog-заказ по номеру из магазина в карточке кабинета.
Схемные ошибки, несоответствие номера и дубликаты закрываются как
недоступность провайдера.

## Alternatives considered

Переиспользовать `logistics.orders.read` отвергнуто: поиск по merchant ID и
поиск по provider order ID — разные контракты и разные поля запроса.
Возвращать raw response отвергнуто из-за PII и нестабильной схемы провайдера.

## Consequences

Поиск заказа по номеру магазина доступен через общий connector runtime и
generated SDK, а непроверенные поля ответа остаются fail-closed.
