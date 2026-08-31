# ADR-0154 — справочник ПВЗ 5Post

Status: Accepted

## Context

Официальный API Партнеров 5post v7.32 документирует JWT exchange и
`POST /api/v1/pickuppoints/query`. Ранее runtime допускал только проверку
кабинета, хотя SDK уже имел provider-neutral `PickupPointReader`. Включать
создание отправлений и другие записи без отдельной проверки их payload и
идемпотентности было бы небезопасно.

## Decision

Допустить для 5Post только `pickup.points.read`. Host получает короткоживущий
JWT по API key внутри callback SecretProvider, запрашивает одну bounded страницу
ПВЗ на фиксированном HTTPS host, применяет country/city filter и возвращает
только `PickupPoint`. `accept-language: ru` используется для стабильной
локализации ошибок. JWT и raw provider response не покидают host transport.

## Security and privacy impact

API key и JWT остаются callback-scoped; remote identifiers не становятся
каноническими warehouse IDs. Tenant, account и capability checks остаются в
registry и connector. Адрес ПВЗ — публичные данные каталога, но raw response и
лишние provider fields не сохраняются в Core.

## Compatibility impact

Изменение аддитивное: в runtime support появляется только уже объявленная
manifest capability `pickup.points.read`. Existing shipment, tracking and label
routes не меняются.

## Operational impact

Один вызов справочника выполняет JWT exchange и bounded page read. Результат
ограничен 500 точками нейтральным контрактом, provider limit 1000 не обходится.
Remote rate limits и timeout обрабатываются общим host HTTP transport.

## Migration and data impact

Миграция не требуется. Используются существующие account, SecretProvider и
pickup read route.

## Alternatives considered

Оставить ПВЗ fail-closed отвергнуто: официальный контракт доступен и
provider-neutral read surface уже существует. Включить shipment writes вместе
с ПВЗ отвергнуто: это потребует отдельной проверки payment, warehouse,
barcode, idempotency и ambiguous-write поведения.

## Consequences

Оператор может выбрать ПВЗ 5Post через bounded read. Остальные операции
остаются явно закрытыми и требуют отдельных qualification tasks.
