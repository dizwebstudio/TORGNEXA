# ADR-0157 — PDF-этикетка 5Post

Status: Accepted

## Context

Официальный API Партнеров 5post v7.32 документирует асинхронное получение
этикетки через `POST /api/v1/orderLabels/byOrderId` с `format=PDF`. Ответом
является PDF-файл, а этикетка может быть ещё не готова и вернуться с ошибкой
409.

## Decision

Допустить для 5Post только `logistics.label.read` в формате `pdf` для одного
provider order UUID. Host получает JWT по API key в callback SecretProvider,
отправляет один `orderIds` элемент, проверяет HTTP success, PDF MIME и `%PDF-`
signature, затем возвращает только digest reference. Бинарное тело, JWT и
provider payload не покидают host transport.

## Security and privacy impact

API key и JWT остаются callback-scoped. UUID проходит строгую проверку до
включения в path/body. PDF не сохраняется в connector result и не попадает в
Core, events или logs; выдача артефакта использует существующий opaque
reference boundary.

## Compatibility impact

Изменение аддитивное: в runtime support появляется уже объявленная manifest
capability `logistics.label.read`. Existing create, cancel and track routes не
изменяются.

## Operational impact

Операция ограничена одной этикеткой и уважает общий timeout/response-size
boundary. Ответ 409/not-ready и любой не-PDF результат остаются ошибкой для
оператора и могут быть повторены существующим application workflow после
ожидания готовности.

## Migration and data impact

Миграция не требуется. Используются существующие label route,
SecretProvider, operation receipt и artifact reference contract.

## Alternatives considered

Включить ZIP или массовые labels отвергнуто: это потребует отдельного
multi-order contract и artifact unpacking boundary. Принимать любой 2xx без
проверки PDF отвергнуто: такой ответ не гарантирует пригодный документ.

## Consequences

Оператор может запросить одну PDF-этикетку 5Post через существующий маршрут.
Создание заказа и массовая/ZIP печать требуют отдельных qualification tasks.
