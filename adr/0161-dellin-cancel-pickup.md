# ADR-0161 — отмена забора от адреса «Деловых Линий»

Status: Accepted

## Context

API «Деловых Линий» различает отмену доставки до адреса получателя
(`cancel_delivery.json`) и отмену забора от адреса отправителя
(`cancel_pickup.json`). Оба метода принимают числовой `orderID` и возвращают
`data.status=success` как принятие асинхронной заявки, а не окончательное
состояние заказа.

## Decision

Расширить существующую neutral shipment-cancellation команду необязательным
`variant`: `delivery` или `pickup`. Пустое значение канонизируется в
`delivery`, поэтому существующие клиенты не меняются. API принимает optional
JSON body, core переносит выбранный режим в tenant-scoped outbox event, а worker
передаёт его в connector adapter после обычных approval, capability, account,
tenant и idempotency checks.

Для Dellin `delivery` вызывает `/v3/orders/cancel_delivery.json`, `pickup`
вызывает `/v3/orders/cancel_pickup.json`. Оба результата нормализуются в
`cancellation_pending`; последующее tracking/reconciliation чтение остаётся
источником финального решения. Другие перевозчики получают прежний пустой
variant и сохраняют свои адаптерные контракты.

## Security and privacy impact

Variant — bounded enum без PII и секретов. В provider request остаются только
appkey, временный sessionID, числовой order ID и `requester=sender`; PAT и
сырой ответ не выходят из callback-scoped transport. Outbox содержит только
нейтральный enum режима.

## Compatibility impact

Изменение аддитивное: optional request body и optional event property не меняют
старых callers; empty body продолжает означать `delivery`, а digest этого
режима сохраняет прежнюю idempotency-идентичность. Сгенерированные SDK получают
возможность передавать body для новых клиентов.

## Migration and data impact

Миграция не требуется. Режим живёт в event payload и operation digest на время
одной команды; новые таблицы и provider-specific persistent fields не нужны.

## Operational impact

Оператор выбирает «Доставка до адреса» или «Забор от адреса» в карточке отмены
Dellin. Для обоих методов ответ перевозчика остаётся pending до отдельной
сверки; отмена терминального заказа и возвраты остаются закрытыми.

## Alternatives considered

Выводить режим из `service_code` отвергнуто: shipment service не является
надёжным признаком операции отмены. Создать отдельный provider-specific endpoint
отвергнуто: текущий approval-bound neutral cancellation workflow уже выражает
общую семантику и изолирует provider choice в adapter.

## Consequences

Оператор может безопасно запросить оба документированных address-service
варианта отмены Dellin, не получая ложного финального `cancelled` и не ослабляя
границы approval, idempotency, tenant и SecretProvider.
