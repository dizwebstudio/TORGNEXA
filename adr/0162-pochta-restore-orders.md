# ADR-0162 — возврат заказов Почты России в список «Новые»

Status: Accepted

## Context

Официальное API «Отправка» позволяет вернуть заказы из сформированной партии
обратно в список «Новые» через `POST /1.0/user/backlog`. Это отдельная семантика:
она не отменяет заказ и не возвращает посылку отправителю. Ответ может содержать
поэлементные ошибки, поэтому частичное подтверждение нельзя выдавать за успех.

## Decision

Добавить нейтральную capability `logistics.orders.restore` и порт
`LogisticsOrderRestorer`. Host-маршрут `POST /api/v1/logistics/orders/restore`
требует включённую capability, approval с write-sensitive risk и
tenant-scoped idempotency receipt. Adapter отправляет 1–100 числовых order ID
в официальный `POST /1.0/user/backlog`, принимает только полный совпадающий
набор `result-ids` без `errors` и нормализует его в `restored`.

## Security and privacy impact

Операция не добавляет PII или секретов в контракты. Credentials используются
только callback-scoped SecretProvider и фиксированным HTTPS transport. При
неоднозначном внешнем исходе operation receipt запрещает слепой повтор.

## Compatibility impact

Изменение аддитивное: новая capability и новый endpoint не меняют существующую
отмену заказа, формирование партии или архивные операции.

## Migration and data impact

Миграция не требуется. Используются существующие approval и operation receipt;
в результате сохраняются только идентификаторы, статус и время наблюдения.

## Operational impact

Оператор вводит идентификаторы сформированных заказов и заранее одобренный
запрос в карточке кабинета Почты России. Частичный ответ показывается как
ошибка провайдера и требует ручной сверки.

## Alternatives considered

Расширить `cancel` отвергнуто: это разные операции провайдера. Разрешить
частичный `result-ids` отвергнуто: он не доказывает возврат всего набора.

## Consequences

Сформированные заказы можно безопасно вернуть в следующую партию через общий
approval/idempotency контур, не раскрывая provider payload и не смешивая
семантики отмены и возврата.
