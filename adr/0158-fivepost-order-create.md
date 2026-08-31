# ADR-0158 — Универсальное создание заказа 5Post

Status: Accepted

## Context

Официальный API Партнеров 5Post v7.32 рекомендует `POST /api/v3/orders` для
создания одноместных и многоместных заказов. Запрос требует не только адресов и
габаритов, но и provider-owned склада отправителя, точки выдачи, стоимости,
товарных строк с НДС и режима генерации штрихкодов.

## Decision

Допустить для 5Post только однопосылочное создание заказа в точку выдачи.
Нейтральный SDK получает явные товарные строки, объявленную стоимость, стоимость
доставки и сумму к оплате. Конфигурация аккаунта отдельно содержит
`sender_location`, `return_location`, `undeliverable_option` и
`barcode_enrichment`; эти значения не выводятся из адресов или ПВЗ.

Host отправляет один `partnerOrders` и один cargo, принимает только HTTP 2xx с
`code=10`, одним совпадающим `orderId`/`senderOrderId` и одним совпадающим
cargo identity. Неоднозначный ответ не повторяется автоматически.

## Security and privacy impact

API key и JWT остаются callback-scoped. Внешний запрос содержит только данные,
явно переданные в shipment command; raw response, credentials и лишние поля не
попадают в Core/audit. Платёжные и товарные суммы проверяются точными minor
units до сериализации в decimal JSON.

## Compatibility impact

Изменение аддитивное: существующий `ShipmentCreateRequest` получает optional
поля, а runtime support допускает уже заявленную manifest capability
`logistics.shipment.create` для 5Post. Другие перевозчики эти поля игнорируют.

## Migration and data impact

Миграция не требуется: товарные строки сохраняются внутри существующего
callback-scoped shipment payload. В production аккаунту потребуется явная
не секретная конфигурация 5Post и отдельное подтверждение тестового заказа.

## Operational impact

Создание ограничено одним cargo и одной точкой выдачи; тарифный preview,
многоместность и неоднозначные внешние исходы остаются закрытыми.

## Alternatives considered

Выводить склад из `from` или считать ПВЗ складом отвергнуто: это разные
идентификаторы 5Post. Отправлять package-only заказ отвергнуто: официальный
endpoint требует cost/cargo productValues. Многоместный режим отложен до
отдельной географии и qualification evidence.

## Consequences

Оператор может создать один полностью описанный заказ 5Post через существующий
approval-bound shipment workflow. Для следующего заказа потребуется отдельная
идемпотентная команда, а не повтор внешнего вызова после неизвестного результата.
