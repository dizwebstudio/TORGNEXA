# Спецификация коннектора «Деловые Линии»

- идентификатор: `dellin`
- семейство: `logistics`
- версия SDK: `1`
- авторизация для проверки: JSON `{ "appkey": "…", "pat": "…" }`
- endpoint проверки: `https://api.dellin.ru/v4/auth/login.json`

Проверка выполняет официальный token-based login и убеждается, что API вернул
непустой `sessionID`. Значение сессии является временным и не покидает
callback проверки.

В runtime включены bounded read-only чтение справочника терминалов/ПВЗ
(`pickup.points.read`), предпросмотр тарифа (`logistics.rates.read`) и единичное
чтение истории статусов (`logistics.track.read`): сначала выполняется `POST
https://api.dellin.ru/v3/public/terminals.json` с appkey, затем загружается
возвращённый официальный URL каталога на том же host и из выбранного города
нормализуется не более запрошенного лимита пунктов. Внешний URL принимается
только при точном host `api.dellin.ru` и пути
`/catalog/terminals_v3.json`; remote ID не становится ID склада в Core.

Для tracking выполняется `POST https://api.dellin.ru/v3/orders/statuses_history.json`
с одним `docIds`. Ответ ограничивается 100 событиями, проверяется по ключу
документа и нормализуется в последний статус; сырой ответ и клиентские данные
не выходят из host-side transport. Для rates выполняется `POST
https://api.dellin.ru/v2/calculator.json` после временного login; адреса и до 50
мест преобразуются в bounded payload, цена принимается только как неотрицательное
десятичное значение с точностью до копейки, а `priceMinimal` ограничен известными
типами доставки. Операция создания отправления допускает address-to-address и
bounded terminal-to-terminal сценарии: для второго runtime-конфигурация явно
задаёт `sender_terminal_id`, а `pickup_point_ref` в нейтральном запросе задаёт
терминал получения; оба значения являются числовыми remote references. Отмена
адресной доставки выполняется через официальный `POST
/v3/orders/cancel_delivery.json`, а отмена забора от адреса — через `POST
/v3/orders/cancel_pickup.json`; нейтральный режим выбирается как
`delivery`/`pickup`. Ответ `data.status=success`
означает только принятие заявки, поэтому локальный статус становится
`cancellation_pending`, пока история статусов не подтвердит или отклонит
отмену. Отмена терминального заказа и возвраты остаются закрытыми.

Для Pre-Alert доступна отдельная отмена пакетной заявки через `POST
/v2/batch_request/cancel.json`; она требует числовой `batchRequestID` и
нормализуется в `CANCELLED`. Отмена отдельной терминальной перевозки и
возвраты остаются закрытыми.

Заявленные в SDK capability (`logistics.rates.read`, `logistics.shipment.create`,
`logistics.shipment.cancel`, `logistics.track.read` и `logistics.label.read`)
не означают автоматическую доступность операций в production runtime. Сейчас
runtime support явно включает `pickup.points.read`, `logistics.rates.read`,
`logistics.track.read` и ограниченное `logistics.shipment.create` на поверхности
`separate_surface/logistics`. Для создания обязательна конфигурация
`requester_uid`, `sender_counteragent_id`, `freight_uid`, необязательный
`sender_terminal_id`, `produce_date`, `derival_worktime_start`,
`derival_worktime_end` и `payment_type`. Операция
использует address-to-address или terminal-to-terminal запрос с анонимным получателем; отмена
адресной доставки использует `orderID` и `requester=sender`, а её результат
нормализуется в `cancellation_pending`. Отмена терминального заказа, возвраты и
другие неподтверждённые операции остаются fail-closed.

`logistics.label.read` использует официальный `POST
https://api.dellin.ru/v1/printable.json` с `docUID` и `mode=order`. Входом
является UID накладной из `orders.documents.uid`, а не номер заявки создания.
Host декодирует base64, проверяет сигнатуру PDF и возвращает только
контентно-адресуемый `artifact_ref`; URL провайдера и PDF-тело не покидают
transport.
