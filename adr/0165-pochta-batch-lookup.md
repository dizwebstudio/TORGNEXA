# ADR-0165 — поиск партии Почты России по имени

Status: Accepted

## Context

API «Отправка» предоставляет отдельный read-only lookup партии по её
provider name. Ответ не должен автоматически становиться raw extension data
в Core.

## Decision

Переиспользовать `logistics.batches.read` и добавить порт
`LogisticsBatchLookupReader` с host route
`GET /api/v1/logistics/batches/{batch_id}`. Runtime вызывает
`GET /1.0/batch/{batch-name}` без query/body, принимает только числовое имя и
ровно одну запись, сверяет `batch-name` и возвращает `LogisticsBatch`.

## Security and privacy impact

Проекция ограничена ID партии, статусом, количеством отправлений и UTC
observation time. Состав заказов, raw payload и credentials не выходят из
host transport. Действуют tenant scope, `connectors.read` и enabled
`logistics.batches.read`.

## Compatibility impact

Изменение аддитивное и не меняет список партий, batch writes, restore или
order lookup.

## Migration and data impact

Миграция не требуется: операция read-only и не создаёт записи.

## Operational impact

Оператор может быстро проверить одну партию по номеру в карточке кабинета.
Ошибки schema или mismatch показываются как недоступность провайдера.

## Alternatives considered

Возвращать raw JSON отвергнуто из-за нестабильности и лишних полей.
Использовать только список партий отвергнуто: это не заменяет точечный lookup.

## Consequences

Проверка партии стала доступна в SDK/UI с минимальной projection, а любые
дополнительные поля остаются fail-closed.
