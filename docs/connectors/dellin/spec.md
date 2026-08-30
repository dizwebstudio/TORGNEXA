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
(`pickup.points.read`) и единичное чтение истории статусов
(`logistics.track.read`): сначала выполняется `POST
https://api.dellin.ru/v3/public/terminals.json` с appkey, затем загружается
возвращённый официальный URL каталога на том же host и из выбранного города
нормализуется не более запрошенного лимита пунктов. Внешний URL принимается
только при точном host `api.dellin.ru` и пути
`/catalog/terminals_v3.json`; remote ID не становится ID склада в Core.

Для tracking выполняется `POST https://api.dellin.ru/v3/orders/statuses_history.json`
с одним `docIds`. Ответ ограничивается 100 событиями, проверяется по ключу
документа и нормализуется в последний статус; сырой ответ и клиентские данные
не выходят из host-side transport. Расчёт и операции отправлений остаются
закрытыми до qualification.

Заявленные в SDK capability (`logistics.rates.read`, `logistics.shipment.create`,
`logistics.shipment.cancel`, `logistics.track.read` и `logistics.label.read`)
не означают автоматическую доступность операций в production runtime. Сейчас
runtime support явно включает только `pickup.points.read` и
`logistics.track.read` на поверхности `separate_surface/logistics`; расчёт,
оформление, отмена и этикетки остаются fail-closed до отдельной qualification.
