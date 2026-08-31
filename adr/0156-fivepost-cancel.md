# ADR-0156 — отмена заказа 5Post

Status: Accepted

## Context

Официальный API Партнеров 5post v7.32 документирует `DELETE
/api/v2/cancelOrder/byOrderId/{orderId}`. Endpoint возвращает HTTP 200 и
отдельный бизнес-флаг `error`, поэтому одного HTTP статуса недостаточно для
подтверждения отмены.

## Decision

Допустить для 5Post только `logistics.shipment.cancel`. Host получает JWT по
API key в callback SecretProvider, проверяет provider order UUID, вызывает
официальный fixed-host endpoint и принимает только `error=false`. При
`canBeRetriedLater=true` ошибка возвращается оператору как retryable provider
failure; терминальный отказ не превращается в успешную отмену. Host
idempotency/approval receipt остаются внешней границей повторных вызовов.

## Security and privacy impact

API key и JWT остаются callback-scoped. В URL попадает только проверенный
provider UUID; provider error text не сохраняется в Core projection. Нет
передачи клиентской PII или raw response.

## Compatibility impact

Изменение аддитивное: в runtime support появляется уже объявленная manifest
capability `logistics.shipment.cancel`. Create, track and label routes не
изменяются.

## Operational impact

Операция выполняется для одного заказа и не повторяется транспортом после
неопределённого сетевого результата; общий host operation receipt управляет
повтором на уровне приложения. Provider business errors остаются видимыми.

## Migration and data impact

Миграция не требуется. Используются существующие shipment cancel worker,
SecretProvider, approval и operation receipt boundaries.

## Alternatives considered

Оставить cancel закрытым отвергнуто: официальный endpoint и exact business
acknowledgement позволяют bounded admission. Считать любой HTTP 200 успехом
отвергнуто: API явно различает `error=false` и ошибку в теле.

## Consequences

Оператор может отменить один уже известный заказ 5Post через существующий
approval-bound маршрут. Создание и этикетки требуют следующих отдельных задач.
