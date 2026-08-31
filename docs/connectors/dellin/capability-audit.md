# Аудит возможностей «Деловых Линий»

Официальная документация подтверждает два способа авторизации Public API:
по номеру телефона и паролю (`v3/auth/login.json`) либо по appkey и
персональному токену (`v4/auth/login.json`). Для TORGNEXA выбран PAT: он не
требует хранить пароль и действует ограниченный срок, заданный перевозчиком.

Подтверждённые источники:

- [Авторизация пользователя](https://dev.dellin.ru/api/auth/login/) — appkey + PAT и временный sessionID;
- [Калькулятор стоимости](https://dev.dellin.ru/api/calculation/calculator/) — `v2/calculator.json`;
- [Оформление заказа](https://dev.dellin.ru/api/examples/request/) — `v2/request.json`;
- [Журнал заказов](https://dev.dellin.ru/api/orders/search/) — `v3/orders.json`;
- [История статусов заказа](https://dev.dellin.ru/api/orders/statuses-history/) — `v3/orders/statuses_history.json`;
- [Печатные формы документов](https://dev.dellin.ru/api/orders/print/) — `v1/printable.json`;
- [Справочник терминалов](https://dev.dellin.ru/api/terminals/directory/) — `v3/public/terminals.json`.
- [Pre-Alert: пакетный заказ](https://dev.dellin.ru/api/ordering/pre-alert/) — `v2/batch_request/cancel.json`.

В текущем runtime включены credential-проверка, bounded read-only чтение
справочника терминалов/ПВЗ, `logistics.rates.read` и единичное
`logistics.track.read`. Rates использует официальный калькулятор с текстовыми
адресами, агрегированными габаритами и весом не более 50 мест; `freightUID` не
передаётся, поскольку для этого bounded preview он необязателен. Tracking
использует официальный `POST /v3/orders/statuses_history.json` с `appkey` и
одним `docIds`, ограничивает историю 100 событиями, выбирает последнюю дату и
не переносит сырой ответ или данные клиента в Core. Address-to-address
оформление использует `POST /v2/request.json`, временный `sessionID`, явные
UID контрагента/характера груза и дату с окном передачи; ответ допускается
только при `state=success` и числовом `requestID`. Дополнительно разрешён bounded
маршрут terminal-to-terminal: `derival.terminalID` берётся из явной
`sender_terminal_id`, `arrival.terminalID` — из `pickup_point_ref`; адресные
варианты по-прежнему используют только address-to-address форму. Отмена адресной доставки
вызывается через официальный `POST /v3/orders/cancel_delivery.json`; для
отмены забора от адреса используется `POST /v3/orders/cancel_pickup.json`.
Нейтральный режим выбирается как `delivery` или `pickup`; ответ
`data.status=success` означает приём заявки, поэтому runtime возвращает
`cancellation_pending`, а не ложный терминальный `cancelled`. Последующая
сверка истории статусов должна подтвердить или отклонить отмену. Отмена
Для Pre-Alert подтверждена отдельная операция расформирования пакетной заявки:
`POST https://api.dellin.ru/v2/batch_request/cancel.json` принимает
`batchRequestID` и возвращает `data.state=success`. Она допущена как
`logistics.batches.cancel` и не является отменой отдельного терминального
заказа. Отмена отдельного терминального заказа и возвраты требуют отдельных
fixtures, маппинга адресов/терминалов и остаются закрытыми.

Печатная форма накладной допущена отдельно: transport вызывает `v1/printable.json`
с `docUID` из журнала заказов и `mode=order`, принимает ровно один документ,
проверяет совпадение UID и PDF после base64-декодирования, а затем возвращает
контентно-адресуемый opaque reference. URL из ответа провайдера игнорируется.
