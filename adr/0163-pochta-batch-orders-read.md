# ADR-0163 — чтение заказов Почты России внутри партии

Status: Accepted

## Context

Официальное API «Отправка» предоставляет отдельный read-only запрос состава
партии через `GET /1.0/batch/{batch-name}/shipment`. Ответ содержит поля,
которые могут включать данные получателя и адреса, поэтому он не должен
целиком пересекать границу connector SDK.

## Decision

Добавить capability `logistics.batches.orders.read`, порт
`LogisticsBatchOrderReader` и host route
`GET /api/v1/logistics/batches/orders`. Runtime принимает только числовой
batch ID, bounded page/limit и фиксированную сортировку. В нейтральную модель
попадают provider order ID, batch ID, barcode, lowercase status и UTC
observation time. Exact batch match и duplicate rejection обязательны.

## Security and privacy impact

Получатель, адрес, телефон, сырой provider payload и credentials не выходят из
host transport и не сохраняются в SDK projection. Маршрут требует обычной
tenant-scoped authenticated read permission и включённой capability.

## Compatibility impact

Изменение аддитивное: новая read-only capability и OpenAPI operation не меняют
существующие партии, restore, cancel или shipment routes.

## Migration and data impact

Миграция не требуется. Операция не создаёт записи и не использует idempotency
receipt.

## Operational impact

Оператор может проверить состав партии в карточке кабинета. Ошибка schema,
несовпадение партии или превышение лимита отображаются как недоступность
провайдера и требуют ручной проверки.

## Alternatives considered

Возвращать raw provider JSON отвергнуто из-за PII и нестабильного контракта.
Объединять список строк с batch-directory отвергнуто: это разные лимиты,
endpoint и семантика провайдера.

## Consequences

Состав партии доступен для UI и SDK в минимальной безопасной форме, а
неподтверждённые provider fields остаются fail-closed.
