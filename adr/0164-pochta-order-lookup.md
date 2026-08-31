# ADR-0164 — поиск заказа Почты России по ID внутри партии

Status: Accepted

## Context

Официальное API «Отправка» содержит отдельный read-only метод поиска заказа в
партии по внутреннему ID. Его ответ может содержать адреса и данные получателя,
которые не должны становиться частью нейтрального connector SDK.

## Decision

Добавить capability `logistics.orders.read`, порт `LogisticsOrderReader` и
host route `GET /api/v1/logistics/orders/{order_id}`. Runtime вызывает
`GET /1.0/shipment/{id}` без caller-controlled URL, принимает только числовой
ID и ровно одну provider record, сверяет ID ответа и возвращает безопасную
`LogisticsBatchOrder` projection с batch ID, barcode, lowercase status и UTC
observation time.

## Security and privacy impact

Получатель, адрес, телефон, сырой provider payload и credentials не выходят из
host transport. Маршрут требует tenant scope, `connectors.read` и включённую
capability; approval и idempotency не применяются к read-only запросу.

## Compatibility impact

Изменение аддитивное: новая capability и OpenAPI operation не меняют список
заказов партии, restore, cancel или shipment routes.

## Migration and data impact

Миграция не требуется. Операция не создаёт запись и не использует operation
receipt.

## Operational impact

Оператор может найти один заказ по ID в карточке кабинета. Ошибка схемы,
несовпадение ID или лишние строки отображаются как недоступность провайдера.

## Alternatives considered

Возвращать raw provider JSON отвергнуто из-за PII и нестабильных полей.
Переиспользовать batch list отвергнуто: provider endpoint и bounded semantics
для точечного поиска отличаются.

## Consequences

Точечная проверка заказа доступна без раскрытия provider payload, а любой
неоднозначный ответ остаётся fail-closed.
