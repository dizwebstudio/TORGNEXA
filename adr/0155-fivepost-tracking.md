# ADR-0155 — чтение статуса заказа 5Post

Status: Accepted

## Context

Официальный API Партнеров 5post v7.32 документирует `POST
/api/v1/getOrderStatus`. SDK уже имел provider-neutral tracker, но runtime
оставлял 5Post status lookup закрытым вместе с write-операциями.

## Decision

Допустить для 5Post только `logistics.track.read`. Host получает JWT по API key
в callback SecretProvider, отправляет массив ровно с одним `orderId`, проверяет
ровно один ответ с тем же `orderId`, разбирает `changeDate` как RFC3339 и
возвращает нейтральный `ShipmentResult`. `executionStatus` и `errorDesc` не
покидают host transport.

## Security and privacy impact

API key и JWT остаются callback-scoped. Remote order ID проверяется до
нормализации; raw response, rejection text и клиентские данные не пишутся в
Core, события или логи.

## Compatibility impact

Изменение аддитивное: в runtime support появляется уже объявленная manifest
capability `logistics.track.read`. Existing create/cancel/label routes не
меняются.

## Operational impact

Операция read-only и bounded одним заказом. Общий host transport сохраняет
HTTPS fixed-host, timeout, response-size и Retry-After boundary; provider
rate-limit ошибки не превращаются в успешный статус.

## Migration and data impact

Миграция не требуется. Используются существующие account, SecretProvider и
tracking route.

## Alternatives considered

Оставить tracking закрытым отвергнуто: официальный контракт и typed adapter
достаточны для bounded read. Передавать `executionStatus` в Core отвергнуто:
это provider-local detail, а neutral SDK должен сохранять только status
projection.

## Consequences

Оператор может запросить актуальный статус заказа 5Post. История, callbacks и
write/document operations требуют отдельных qualification tasks.
